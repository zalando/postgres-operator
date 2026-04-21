#!/bin/sh

set -ex

if [ -z "${CONNECTION_POOLER_CLIENT_TLS_CRT}" ]; then
    openssl req -nodes -new -x509 -subj /CN=spilo.dummy.org \
        -keyout /etc/ssl/certs/pgbouncer.key \
        -out /etc/ssl/certs/pgbouncer.crt
else
    ln -s ${CONNECTION_POOLER_CLIENT_TLS_CRT} /etc/ssl/certs/pgbouncer.crt
    ln -s ${CONNECTION_POOLER_CLIENT_TLS_KEY} /etc/ssl/certs/pgbouncer.key
    if [ ! -z "${CONNECTION_POOLER_CLIENT_CA_FILE}" ]; then
        ln -s ${CONNECTION_POOLER_CLIENT_CA_FILE} /etc/ssl/certs/ca.crt
    fi
fi

envsubst < /etc/pgbouncer/pgbouncer.ini.tmpl > /etc/pgbouncer/pgbouncer.ini
envsubst < /etc/pgbouncer/auth_file.txt.tmpl > /etc/pgbouncer/auth_file.txt

# --- Append Infrastructure Roles ---
if [ -n "${INFRASTRUCTURE_ROLES}" ]; then
    # Use a loop to read each line from the multi-line variable
    echo "${INFRASTRUCTURE_ROLES}" | while IFS= read -r line; do
        # Skip empty lines
        [ -z "${line}" ] && continue

        # Append formatted "user" "password" pair to the auth file
        # This assumes each line of $INFRASTRUCTURE_ROLES is "user password"
        echo "${line}" | awk '{printf "\"%s\" \"%s\"\n", $1, $2}' >> /etc/pgbouncer/auth_file.txt
    done
fi

exec /bin/pgbouncer /etc/pgbouncer/pgbouncer.ini
