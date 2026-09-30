package auth

import "testing"

func TestParseInt(t *testing.T) {
	n, err := parseInt("260000")
	if err != nil || n != 260000 {
		t.Errorf("parseInt(%q) = %d, %v", "260000", n, err)
	}
	_, err = parseInt("abc")
	if err == nil {
		t.Error("expected error for non-integer")
	}
}
