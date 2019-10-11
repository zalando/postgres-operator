#! /usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

# make script trace visible via `kubectl logs`
set -o xtrace

ALL_DB_SIZE_QUERY="select sum(pg_database_size(datname)::numeric) from pg_database;"
PG_BIN=$PG_DIR/$PG_VERSION/bin
DUMP_SIZE_COEFF=5

TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
K8S_API_URL=https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT
CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
CLUSTER_NAME_LABEL=cluster-name

function estimate_size {
    "$PG_BIN"/psql -tqAc "${ALL_DB_SIZE_QUERY}"
}

function dump {
    # settings are taken from the environment
    "$PG_BIN"/pg_dumpall
}

function compress {
    pigz
}

function aws_upload {
    declare -r EXPECTED_SIZE="$1"

    # mimic bucket setup from Spilo
    # to keep logical backups at the same path as WAL
    # NB: $LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX already contains the leading "/" when set by the Postgres Operator
    PATH_TO_BACKUP=s3://$LOGICAL_BACKUP_S3_BUCKET"/spilo/"$SCOPE$LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX"/logical_backups/"$(date +%s).sql.gz

    if [ -z "$EXPECTED_SIZE" ]; then
        aws s3 cp - "$PATH_TO_BACKUP" --debug --sse="AES256"
    else
        aws s3 cp - "$PATH_TO_BACKUP" --debug --expected-size "$EXPECTED_SIZE" --sse="AES256"
    fi;
}

function get_pods {
    declare -r SELECTOR="$1"

    curl "${K8S_API_URL}/api/v1/namespaces/${POD_NAMESPACE}/pods?$SELECTOR"        \
        --cacert $CERT                          \
        -H "Authorization: Bearer ${TOKEN}" | jq .items[].status.podIP -r
}

function get_current_pod {
    curl "${K8S_API_URL}/api/v1/namespaces/${POD_NAMESPACE}/pods?fieldSelector=metadata.name%3D${HOSTNAME}" \
        --cacert $CERT   \
        -H "Authorization: Bearer ${TOKEN}"
}

declare -a search_strategy=(
    get_cluster_name_label
    list_all_replica_pods_current_node
    list_all_replica_pods_any_node
    get_master_pod
)

function get_config_resource() {
    curl "${K8S_API_URL}/apis/apps/v1/namespaces/default/deployments/postgres-operator" \
        --cacert $CERT   \
        -H "Authorization: Bearer ${TOKEN}" | jq '.spec.template.spec.containers[0].env[] | select(.name == "$1") | .value'
}

function get_cluster_name_label {
    local config
    local clustername

    config=$(get_config_resource "CONFIG_MAP_NAME")
    if [ -n "$config" ]; then
        clustername=$(curl "${K8S_API_URL}/api/v1/namespaces/default/configmaps/${config}" \
                            --cacert $CERT   \
                            -H "Authorization: Bearer ${TOKEN}" | jq '.data.cluster_name_label')
    else
        config=$(get_config_resource "POSTGRES_OPERATOR_CONFIGURATION_OBJECT")
        if [ -n "$config" ]; then
            clustername=$(curl "${K8S_API_URL}/apis/acid.zalan.do/v1/namespaces/default/operatorconfigurations/${config}" \
                                --cacert $CERT   \
                                -H "Authorization: Bearer ${TOKEN}" | jq '.configuration.kubernetes.cluster_name_label')
        fi
    fi

    if [ -n "$clustername" ]; then
        CLUSTER_NAME_LABEL=${clustername}
    fi;
}

function list_all_replica_pods_current_node {
    get_pods "labelSelector=${CLUSTER_NAME_LABEL}%3D${SCOPE},spilo-role%3Dreplica&fieldSelector=spec.nodeName%3D${CURRENT_NODENAME}" | head -n 1
}

function list_all_replica_pods_any_node {
    get_pods "labelSelector=${CLUSTER_NAME_LABEL}%3D${SCOPE},spilo-role%3Dreplica" | head -n 1
}

function get_master_pod {
    get_pods "labelSelector=${CLUSTER_NAME_LABEL}%3D${SCOPE},spilo-role%3Dmaster" | head -n 1
}

CURRENT_NODENAME=$(get_current_pod | jq .items[].spec.nodeName --raw-output)
export CURRENT_NODENAME

for search in "${search_strategy[@]}"; do

    PGHOST=$(eval "$search")
    export PGHOST

    if [ -n "$PGHOST" ]; then
        break
    fi

done

dump | compress | aws_upload $(($(estimate_size) / DUMP_SIZE_COEFF))
