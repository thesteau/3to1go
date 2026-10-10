package integrations

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var ErrNotFound = errors.New("integration not found")

type Manager struct {
	mu                 sync.RWMutex
	app, path, keyPath string
	externalKey        bool
	key                []byte
	destinations       []storedDestination
	logger             *slog.Logger
	tlsConfig          func() *tls.Config
	queue              chan Event
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan struct{}
}

func New(app, configDir string, logger *slog.Logger, tlsConfig func() *tls.Config) (*Manager, error) {
	keyPath := strings.TrimSpace(os.Getenv("INTEGRATIONS_KEY_FILE"))
	externalKey := keyPath != ""
	if keyPath == "" {
		keyPath = filepath.Join(configDir, "integrations", "key")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{app: app, path: filepath.Join(configDir, "integrations", "destinations.enc"), keyPath: keyPath, externalKey: externalKey, logger: logger, tlsConfig: tlsConfig, ctx: ctx, cancel: cancel, queue: make(chan Event, 128), done: make(chan struct{})}
	if err := m.load(); err != nil {
		cancel()
		return nil, err
	}
	go m.worker()
	return m, nil
}

func (m *Manager) Close() {
	if m != nil {
		m.cancel()
		<-m.done
	}
}

func public(d storedDestination) Destination {
	p := d.Destination
	p.Events = slices.Clone(p.Events)
	p.URLConfigured, p.HeadersConfigured = d.URL != "", len(d.Headers) > 0
	return p
}

func (m *Manager) Snapshot() []Destination {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Destination, 0, len(m.destinations))
	for _, d := range m.destinations {
		result = append(result, public(d))
	}
	return result
}

func (m *Manager) Save(update Update) (Destination, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := storedDestination{Destination: update.Destination}
	index := -1
	for i, old := range m.destinations {
		if update.ID == old.ID {
			index = i
			d.URL, d.Headers = old.URL, old.Headers
			break
		}
	}
	if update.ID != "" && index < 0 {
		return Destination{}, ErrNotFound
	}
	if index < 0 && len(m.destinations) >= 10 {
		return Destination{}, errors.New("at most ten integrations are supported")
	}
	if d.ID == "" {
		d.ID = randomID()
	}
	if d.ID == "" {
		return Destination{}, errors.New("cannot create integration")
	}
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len(d.Name) > 100 {
		return Destination{}, errors.New("integration name must contain 1 to 100 characters")
	}
	if !slices.Contains([]string{"json", "text", "discord"}, d.Format) {
		return Destination{}, errors.New("choose JSON, plain text, or Discord message format")
	}
	if len(d.Events) == 0 || len(d.Events) > 5 {
		return Destination{}, errors.New("select at least one supported event")
	}
	for _, event := range d.Events {
		if !slices.Contains(eventsFor(m.app), event) {
			return Destination{}, errors.New("unsupported integration event")
		}
	}
	d.Events = slices.Clone(d.Events)
	slices.Sort(d.Events)
	d.Events = slices.Compact(d.Events)
	if d.TimeoutSeconds == 0 {
		d.TimeoutSeconds = 5
	}
	if d.TimeoutSeconds < 1 || d.TimeoutSeconds > 30 {
		return Destination{}, errors.New("timeout must be between 1 and 30 seconds")
	}
	if len(d.MessageTemplate) > 2000 || len(d.MatchJobName) > 255 || len(d.MatchScoutID) > 255 || len(d.MatchInstanceID) > 255 || len(d.MatchSourceAddress) > 255 {
		return Destination{}, errors.New("integration text is too long")
	}
	if update.URL != nil {
		d.URL = strings.TrimSpace(*update.URL)
	}
	u, err := url.Parse(d.URL)
	if len(d.URL) > 16384 || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return Destination{}, errors.New("destination must be an HTTPS URL without embedded user credentials or a fragment")
	}
	if update.Headers != nil {
		d.Headers = make(map[string]string, len(*update.Headers))
		if len(*update.Headers) > 20 {
			return Destination{}, errors.New("at most twenty headers are supported")
		}
		for name, value := range *update.Headers {
			if !validHeaderName(name) || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") || slices.Contains([]string{"host", "content-length", "connection", "transfer-encoding", "content-type"}, strings.ToLower(name)) {
				return Destination{}, errors.New("invalid or reserved integration header")
			}
			canonical := http.CanonicalHeaderKey(name)
			if _, exists := d.Headers[canonical]; exists {
				return Destination{}, errors.New("duplicate integration header")
			}
			d.Headers[canonical] = value
		}
	}
	next := slices.Clone(m.destinations)
	if index < 0 {
		next = append(next, d)
	} else {
		next[index] = d
	}
	if err := m.persist(next); err != nil {
		return Destination{}, err
	}
	m.destinations = next
	return public(d), nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, d := range m.destinations {
		if d.ID == id {
			next := append(slices.Clone(m.destinations[:i]), m.destinations[i+1:]...)
			if err := m.persist(next); err != nil {
				return err
			}
			m.destinations = next
			return nil
		}
	}
	return ErrNotFound
}

func randomID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return ""
	}
	return hex.EncodeToString(data)
}

// Publish enqueues best-effort delivery; an offline destination never holds up backups.
func (m *Manager) Publish(event Event) {
	if m == nil {
		return
	}
	event.App, event.Time, event.ID = m.app, timestamp(), randomID()
	select {
	case <-m.ctx.Done():
	case m.queue <- event:
	default:
		m.logger.Warn("integration_queue_full", "event", event.Type)
	}
}

func (m *Manager) worker() {
	defer close(m.done)
	for {
		select {
		case <-m.ctx.Done():
			return
		case event := <-m.queue:
			m.mu.RLock()
			destinations := slices.Clone(m.destinations)
			m.mu.RUnlock()
			var group sync.WaitGroup
			for _, d := range destinations {
				if !d.Enabled || !slices.Contains(d.Events, event.Type) || (d.MatchScoutID != "" && d.MatchScoutID != event.ScoutID) || (d.MatchInstanceID != "" && d.MatchInstanceID != event.ScoutInstanceID) || (d.MatchJobName != "" && d.MatchJobName != event.JobName) || (d.MatchSourceAddress != "" && d.MatchSourceAddress != event.SourceAddress) {
					continue
				}
				group.Go(func() {
					err := m.deliver(m.ctx, d, event)
					if err != nil {
						m.logger.Warn("integration_delivery_failed", "integration_id", d.ID, "event", event.Type, "detail", err.Error())
					} else {
						m.logger.Info("integration_delivery_succeeded", "integration_id", d.ID, "event", event.Type)
					}
				})
			}
			group.Wait()
		}
	}
}

func (m *Manager) Test(ctx context.Context, id string) error {
	m.mu.RLock()
	var destination *storedDestination
	for _, d := range m.destinations {
		if d.ID == id {
			destination = &d
			break
		}
	}
	m.mu.RUnlock()
	if destination == nil {
		return ErrNotFound
	}
	return m.deliver(ctx, *destination, Event{ID: randomID(), App: m.app, Type: "test", Time: timestamp(), ScoutID: "scout-example", ScoutInstanceID: "instance-example", JobName: "example-job", Status: "test"})
}
