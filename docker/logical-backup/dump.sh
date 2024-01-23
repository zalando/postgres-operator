#! /usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

ALL_DB_SIZE_QUERY="select sum(pg_database_size(datname)::numeric) from pg_database;"
PG_BIN=$PG_DIR/$PG_VERSION/bin
DUMP_SIZE_COEFF=5
ERRORCOUNT=0

TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
KUBERNETES_SERVICE_PORT=${KUBERNETES_SERVICE_PORT:-443}
if [ "$KUBERNETES_SERVICE_HOST" != "${KUBERNETES_SERVICE_HOST#*[0-9].[0-9]}" ]; then
    echo "IPv4"
    K8S_API_URL=https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/api/v1
elif [ "$KUBERNETES_SERVICE_HOST" != "${KUBERNETES_SERVICE_HOST#*:[0-9a-fA-F]}" ]; then
    echo "IPv6"
    K8S_API_URL=https://[$KUBERNETES_SERVICE_HOST]:$KUBERNETES_SERVICE_PORT/api/v1
elif [ -n "$KUBERNETES_SERVICE_HOST" ]; then
    echo "Hostname"
    K8S_API_URL=https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/api/v1
else
  echo "KUBERNETES_SERVICE_HOST was not set"
fi
echo "API Endpoint: ${K8S_API_URL}"
CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

LOGICAL_BACKUP_PROVIDER=${LOGICAL_BACKUP_PROVIDER:="s3"}
LOGICAL_BACKUP_S3_RETENTION_TIME=${LOGICAL_BACKUP_S3_RETENTION_TIME:=""}

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

function az_upload {
    PATH_TO_BACKUP=$LOGICAL_BACKUP_S3_BUCKET"/spilo/"$SCOPE$LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX"/logical_backups/"$(date +%s).sql.gz

    az storage blob upload --file "$1" --account-name "$LOGICAL_BACKUP_AZURE_STORAGE_ACCOUNT_NAME" --account-key "$LOGICAL_BACKUP_AZURE_STORAGE_ACCOUNT_KEY" -c "$LOGICAL_BACKUP_AZURE_STORAGE_CONTAINER" -n "$PATH_TO_BACKUP"
}

function aws_delete_objects {
    args=(
      "--bucket=$LOGICAL_BACKUP_S3_BUCKET"
    )

    [[ ! -z "$LOGICAL_BACKUP_S3_ENDPOINT" ]] && args+=("--endpoint-url=$LOGICAL_BACKUP_S3_ENDPOINT")
    [[ ! -z "$LOGICAL_BACKUP_S3_REGION" ]] && args+=("--region=$LOGICAL_BACKUP_S3_REGION")

    aws s3api delete-objects "${args[@]}" --delete Objects=["$(printf {Key=%q}, "$@")"],Quiet=true
}
export -f aws_delete_objects

function aws_delete_outdated {
    if [[ -z "$LOGICAL_BACKUP_S3_RETENTION_TIME" ]] ; then
        echo "no retention time configured: skip cleanup of outdated backups"
        return 0
    fi

    # define cutoff date for outdated backups (day precision)
    cutoff_date=$(date -d "$LOGICAL_BACKUP_S3_RETENTION_TIME ago" +%F)

    # mimic bucket setup from Spilo
    prefix="spilo/"$SCOPE$LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX"/logical_backups/"

    args=(
      "--no-paginate"
      "--output=text"
      "--prefix=$prefix"
      "--bucket=$LOGICAL_BACKUP_S3_BUCKET"
    )

    [[ ! -z "$LOGICAL_BACKUP_S3_ENDPOINT" ]] && args+=("--endpoint-url=$LOGICAL_BACKUP_S3_ENDPOINT")
    [[ ! -z "$LOGICAL_BACKUP_S3_REGION" ]] && args+=("--region=$LOGICAL_BACKUP_S3_REGION")

    # list objects older than the cutoff date
    aws s3api list-objects "${args[@]}" --query="Contents[?LastModified<='$cutoff_date'].[Key]" > /tmp/outdated-backups

    # spare the last backup
    sed -i '$d' /tmp/outdated-backups

    count=$(wc -l < /tmp/outdated-backups)
    if [[ $count == 0 ]] ; then
      echo "no outdated backups to delete"
      return 0
    fi
    echo "deleting $count outdated backups created before $cutoff_date"

    # deleted outdated files in batches with 100 at a time
    tr '\n' '\0'  < /tmp/outdated-backups | xargs -0 -P1 -n100 bash -c 'aws_delete_objects "$@"' _
}

function aws_upload {
    declare -r EXPECTED_SIZE="$1"

    # mimic bucket setup from Spilo
    # to keep logical backups at the same path as WAL
    # NB: $LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX already contains the leading "/" when set by the Postgres Operator
    PATH_TO_BACKUP=s3://$LOGICAL_BACKUP_S3_BUCKET"/spilo/"$SCOPE$LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX"/logical_backups/"$(date +%s).sql.gz

    args=()

    [[ ! -z "$EXPECTED_SIZE" ]] && args+=("--expected-size=$EXPECTED_SIZE")
    [[ ! -z "$LOGICAL_BACKUP_S3_ENDPOINT" ]] && args+=("--endpoint-url=$LOGICAL_BACKUP_S3_ENDPOINT")
    [[ ! -z "$LOGICAL_BACKUP_S3_REGION" ]] && args+=("--region=$LOGICAL_BACKUP_S3_REGION")
    [[ ! -z "$LOGICAL_BACKUP_S3_SSE" ]] && args+=("--sse=$LOGICAL_BACKUP_S3_SSE")

    aws s3 cp - "$PATH_TO_BACKUP" "${args[@]//\'/}"
}

function gcs_upload {
    PATH_TO_BACKUP=gs://$LOGICAL_BACKUP_S3_BUCKET"/spilo/"$SCOPE$LOGICAL_BACKUP_S3_BUCKET_SCOPE_SUFFIX"/logical_backups/"$(date +%s).sql.gz

    gsutil -o Credentials:gs_service_key_file=$LOGICAL_BACKUP_GOOGLE_APPLICATION_CREDENTIALS cp - "$PATH_TO_BACKUP"
}

function upload {
    case $LOGICAL_BACKUP_PROVIDER in
        "gcs")
            gcs_upload
            ;;
        "s3")
            aws_upload $(($(estimate_size) / DUMP_SIZE_COEFF))
            aws_delete_outdated
            ;;
    esac
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
    get_pods "labelSelector=${CLUSTER_NAME_LABEL}%3D${SCOPE},spilo-role%3Dreplica&fieldSelector=spec.nodeName%3D${CURRENT_NODENAME}" | tee | head -n 1
}

function list_all_replica_pods_any_node {
    get_pods "labelSelector=${CLUSTER_NAME_LABEL}%3D${SCOPE},spilo-role%3Dreplica" | tee | head -n 1
}

function get_master_pod {
    get_pods "labelSelector=${CLUSTER_NAME_LABEL}%3D${SCOPE},spilo-role%3Dmaster" | tee | head -n 1
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

set -x
if [ "$LOGICAL_BACKUP_PROVIDER" == "az" ]; then
    dump | compress > /tmp/azure-backup.sql.gz
    az_upload /tmp/azure-backup.sql.gz
else
    dump | compress | upload
    [[ ${PIPESTATUS[0]} != 0 || ${PIPESTATUS[1]} != 0 || ${PIPESTATUS[2]} != 0 ]] && (( ERRORCOUNT += 1 ))
    set +x

    exit $ERRORCOUNT
fi
