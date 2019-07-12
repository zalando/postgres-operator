#!/usr/bin/env bash
set -e

# NOTE: You still need to start the frontend bits of this application separately
# as starting it here as a child process would leave leftover processes on
# script termination; it appears there is some complication that does not allow
# the shell to clean up nodejs grandchild processes correctly upon script exit.


# Static bits:
export APP_URL="${API_URL-http://localhost:8081}"
export OPERATOR_API_URL="${OPERATOR_API_URL-http://localhost:8080}"
export TARGET_NAMESPACE="${TARGET_NAMESPACE-*}"

default_operator_ui_config='{
  "docs_link":"https://postgres-operator.readthedocs.io/en/latest/",
  "dns_format_string": "{1}-{0}.{2}",
  "databases_visible": true,
  "nat_gateways_visible": false,
  "resources_visible": true,
  "users_visible": true,
  "postgresql_versions": [
    "11",
    "10",
    "9.6"
  ],
  "static_network_whitelist": {
    "localhost": ["172.0.0.1/32"]
  }
}'
export OPERATOR_UI_CONFIG="${OPERATOR_UI_CONFIG-${default_operator_ui_config}}"

# S3 backup bucket:
export SPILO_S3_BACKUP_BUCKET="postgres-backup"

# defines teams
teams='["acid"]'
export TEAMS="${TEAMS-${teams}}"

# Kubernetes API URL (e.g. minikube):
kubernetes_api_url="https://192.168.99.100:8443"

# Enable job control:
set -m

# Clean up child processes on exit:
trap 'kill $(jobs -p)' EXIT


# PostgreSQL Operator UI application name as deployed:
operator_ui_application='postgres-operator-ui'


# Hijack the PostgreSQL Operator UI pod as a proxy for its AWS instance profile
# on the pod's localhost:1234 which allows the WAL-E code to list backups there:
kubectl exec \
  "$(
    kubectl get pods \
      --server="${kubernetes_api_url}" \
      --selector="application=${operator_ui_application}" \
      --output='name' \
      | head --lines=1 \
      | sed 's@^[^/]*/@@'
  )" \
  -- \
    sh -c '
      apk add --no-cache socat;
      pkill socat;
      socat -v TCP-LISTEN:1234,reuseaddr,fork,su=nobody TCP:169.254.169.254:80
    ' \
  &


# Forward localhost:1234 to localhost:1234 on the PostgreSQL Operator UI pod to
# get to the AWS instance metadata endpoint:
echo "Port forwarding to the PostgreSQL Operator UI's instance metadata service"
kubectl port-forward \
  --server="${kubernetes_api_url}" \
  "$(
    kubectl get pods \
      --server="${kubernetes_api_url}" \
      --selector="application=${operator_ui_application}" \
      --output='name' \
      | head --lines=1 \
      | sed 's@^[^/]*/@@'
  )" \
  1234 \
  &


# Forward localhost:8080 to localhost:8080 on the PostgreSQL Operator pod, which
# allows access to the Operator REST API
# when using helm chart use --selector='app.kubernetes.io/name=postgres-operator'
echo 'Port forwarding to the PostgreSQL Operator REST API'
kubectl port-forward \
  --server="${kubernetes_api_url}" \
  "$(
    kubectl get pods \
      --server="${kubernetes_api_url}" \
      --selector='name=postgres-operator' \
      --output='name' \
      | head --lines=1 \
      | sed 's@^[^/]*/@@'
  )" \
  8080 \
  &


# Start a local proxy on localhost:8001 of the target Kubernetes cluster's API:
kubectl proxy &


# Start application:
python3 \
  -m operator_ui \
  --clusters='localhost:8001' \
  $@
