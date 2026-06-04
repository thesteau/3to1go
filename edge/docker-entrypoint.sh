#!/bin/sh
set -eu

ca_dir="${RELAY_EXTRA_CA_DIR:-/run/relay-certs}"
persisted_ca_dir="${RELAY_PERSISTED_CA_DIR:-/config/trusted-certs}"
ca_file="${RELAY_EXTRA_CA_FILE:-}"
trust_target_dir="${RELAY_TRUST_TARGET_DIR:-/usr/local/share/ca-certificates/relay-centralizer}"
update_ca_certificates="${RELAY_UPDATE_CA_CERTIFICATES:-update-ca-certificates}"
installed_count=0

install_ca_file() {
    source_file="$1"
    [ -f "$source_file" ] || return 0

    filename="$(basename "$source_file")"
    case "$filename" in
        *.crt) ;;
        *)
            echo "Skipping extra CA file with non-.crt extension: $filename"
            return 0
            ;;
    esac

    mkdir -p "$trust_target_dir"
    cp "$source_file" "$trust_target_dir/$filename"
    installed_count=$((installed_count + 1))
}

if [ -n "$ca_file" ]; then
    install_ca_file "$ca_file"
fi

if [ -d "$ca_dir" ]; then
    for cert_file in "$ca_dir"/*.crt; do
        [ -e "$cert_file" ] || continue
        install_ca_file "$cert_file"
    done
fi

if [ -d "$persisted_ca_dir" ]; then
    for cert_file in "$persisted_ca_dir"/*.crt; do
        [ -e "$cert_file" ] || continue
        install_ca_file "$cert_file"
    done
fi

if [ "$installed_count" -gt 0 ]; then
    echo "Installed $installed_count extra CA certificate(s). Updating trust store..."
    sh -c "$update_ca_certificates"
fi

exec "$@"
