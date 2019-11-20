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
K8S_API_URL=https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/api/v1
CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

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

function azure_upload {

    # mimic bucket setup from Spilo
    # to keep logical backups at the same path as WAL
    # NB: $LOGICAL_BACKUP_SCOPE_SUFFIX already contains the leading "/" when set by the Postgres Operator
    PATH_TO_BACKUP="spilo/"$SCOPE$LOGICAL_BACKUP_SCOPE_SUFFIX"/logical_backups/"$(date +%s).sql.gz
    LOGICAL_BACKUP_AZURE_CONT_KEY=$(cat $SECRET_MOUNT_PATH/azkey)
    az storage blob upload --file /dev/stdin \
      --account-name $LOGICAL_BACKUP_AZURE_ACC_NAME \
      --container-name $LOGICAL_BACKUP_AZURE_CONT_NAME \
      --account-key $LOGICAL_BACKUP_AZURE_CONT_KEY \
      --auth-mode key \
      --name $PATH_TO_BACKUP
}

function get_pods {
    declare -r SELECTOR="$1"

    curl "${K8S_API_URL}/namespaces/${POD_NAMESPACE}/pods?$SELECTOR" \
        --cacert $CERT \
        -H "Authorization: Bearer ${TOKEN}" | jq .items[].status.podIP -r
}

function get_current_pod {
    curl "${K8S_API_URL}/namespaces/${POD_NAMESPACE}/pods?fieldSelector=metadata.name%3D${HOSTNAME}" \
        --cacert $CERT \
        -H "Authorization: Bearer ${TOKEN}"
}

declare -a search_strategy=(
    list_all_replica_pods_current_node
    list_all_replica_pods_any_node
    get_master_pod
)

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

if [ ! -z "$LOGICAL_BACKUP_S3_BUCKET" ]; then
dump | compress | aws_upload $(($(estimate_size) / DUMP_SIZE_COEFF))
fi

if [ ! -z "$LOGICAL_BACKUP_AZURE_ACC_NAME" ]; then
dump | compress | azure_upload
fi
