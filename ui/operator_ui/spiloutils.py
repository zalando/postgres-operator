from boto3 import client
from datetime import datetime, timezone
from furl import furl
from json import dumps, loads
from os import environ, getenv
from requests import Session
from urllib.parse import urljoin
from uuid import UUID

from .utils import defaulting, these
from operator_ui.adapters.logger import logger

session = Session()

AWS_ENDPOINT = getenv('AWS_ENDPOINT')

OPERATOR_CLUSTER_NAME_LABEL = getenv('OPERATOR_CLUSTER_NAME_LABEL', 'cluster-name')

COMMON_CLUSTER_LABEL = getenv('COMMON_CLUSTER_LABEL', '{"application":"spilo"}')
COMMON_POOLER_LABEL = getenv('COMMON_POOLER_LABEL', '{"application":"db-connection-pooler"}')

logger.info("Common Cluster Label: {}".format(COMMON_CLUSTER_LABEL))
logger.info("Common Pooler Label: {}".format(COMMON_POOLER_LABEL))

COMMON_CLUSTER_LABEL = loads(COMMON_CLUSTER_LABEL)
COMMON_POOLER_LABEL = loads(COMMON_POOLER_LABEL)


def request(cluster, path, **kwargs):
    if 'timeout' not in kwargs:
        # sane default timeout
        kwargs['timeout'] = (5, 15)
    if cluster.cert_file and cluster.key_file:
        kwargs['cert'] = (cluster.cert_file, cluster.key_file)

    return session.get(
        urljoin(cluster.api_server_url, path),
        auth=cluster.auth,
        verify=cluster.ssl_ca_cert,
        **kwargs
    )


def request_post(cluster, path, data, **kwargs):
    if 'timeout' not in kwargs:
        # sane default timeout
        kwargs['timeout'] = 5
    if cluster.cert_file and cluster.key_file:
        kwargs['cert'] = (cluster.cert_file, cluster.key_file)

    return session.post(
        urljoin(cluster.api_server_url, path),
        data=data,
        auth=cluster.auth,
        verify=cluster.ssl_ca_cert,
        **kwargs
    )


def request_put(cluster, path, data, **kwargs):
    if 'timeout' not in kwargs:
        # sane default timeout
        kwargs['timeout'] = 5
    if cluster.cert_file and cluster.key_file:
        kwargs['cert'] = (cluster.cert_file, cluster.key_file)

    return session.put(
        urljoin(cluster.api_server_url, path),
        data=data,
        auth=cluster.auth,
        verify=cluster.ssl_ca_cert,
        **kwargs
    )


def request_delete(cluster, path, **kwargs):
    if 'timeout' not in kwargs:
        # sane default timeout
        kwargs['timeout'] = 5
    if cluster.cert_file and cluster.key_file:
        kwargs['cert'] = (cluster.cert_file, cluster.key_file)

    return session.delete(
        urljoin(cluster.api_server_url, path),
        auth=cluster.auth,
        verify=cluster.ssl_ca_cert,
        **kwargs
    )


def resource_api_version(resource_type):
    return {
        'postgresqls': 'apis/acid.zalan.do/v1',
        'statefulsets': 'apis/apps/v1',
        'deployments': 'apis/apps/v1',
    }.get(resource_type, 'api/v1')


def encode_labels(label_selector):
    return ','.join([
        f'{label}={value}'
        for label, value in label_selector.items()
    ])


def cluster_labels(spilo_cluster):
    labels = COMMON_CLUSTER_LABEL
    labels[OPERATOR_CLUSTER_NAME_LABEL] = spilo_cluster
    return labels


def kubernetes_url(
    resource_type,
    namespace='default',
    resource_name=None,
    label_selector=None,
):

    return furl('/').add(
        path=(
            resource_api_version(resource_type).split('/')
            + (
                ['namespaces', namespace]
                if namespace
                else []
            )
            + [resource_type]
            + (
                [resource_name]
                if resource_name
                else []
            )
        ),
        args=(
            {'labelSelector': encode_labels(label_selector)}
            if label_selector
            else {}
        ),
    ).url


def kubernetes_get(cluster, **kwargs):
    response = request(cluster, kubernetes_url(**kwargs))
    if response.status_code == 404:
        return None
    if response.status_code >= 400:
        response.raise_for_status()
    return response.json()


def read_pods(cluster, namespace, spilo_cluster):
    return kubernetes_get(
        cluster=cluster,
        resource_type='pods',
        namespace=namespace,
        label_selector=cluster_labels(spilo_cluster),
    )


def read_pod(cluster, namespace, resource_name):
    return kubernetes_get(
        cluster=cluster,
        resource_type='pods',
        namespace=namespace,
        resource_name=resource_name,
        label_selector=COMMON_CLUSTER_LABEL,
    )


def read_service(cluster, namespace, resource_name):
    return kubernetes_get(
        cluster=cluster,
        resource_type='services',
        namespace=namespace,
        resource_name=resource_name,
        label_selector=COMMON_CLUSTER_LABEL,
    )


def read_pooler(cluster, namespace, resource_name):
    return kubernetes_get(
        cluster=cluster,
        resource_type='deployments',
        namespace=namespace,
        resource_name=resource_name,
        label_selector=COMMON_POOLER_LABEL,
    )


def read_statefulset(cluster, namespace, resource_name):
    return kubernetes_get(
        cluster=cluster,
        resource_type='statefulsets',
        namespace=namespace,
        resource_name=resource_name,
        label_selector=COMMON_CLUSTER_LABEL,
    )


def read_postgresql(cluster, namespace, resource_name):
    return kubernetes_get(
        cluster=cluster,
        resource_type='postgresqls',
        namespace=namespace,
        resource_name=resource_name,
    )


def read_postgresqls(cluster, namespace):
    return kubernetes_get(
        cluster=cluster,
        resource_type='postgresqls',
        namespace=namespace,
    )


def read_namespaces(cluster):
    return kubernetes_get(
        cluster=cluster,
        resource_type='namespaces',
        namespace=None,
    )


def create_postgresql(cluster, namespace, definition):
    path = kubernetes_url(
        resource_type='postgresqls',
        namespace=namespace,
    )
    try:
        r = request_post(cluster, path, dumps(definition))
        r.raise_for_status()
        return True
    except Exception as ex:
        logger.exception("K8s create request failed")
        return False


def apply_postgresql(cluster, namespace, resource_name, definition):
    path = kubernetes_url(
        resource_type='postgresqls',
        namespace=namespace,
        resource_name=resource_name,
    )
    try:
        r = request_put(cluster, path, dumps(definition))
        r.raise_for_status()
        return True
    except Exception as ex:
        logger.exception("K8s create request failed")
        return False


def remove_postgresql(cluster, namespace, resource_name):
    path = kubernetes_url(
        resource_type='postgresqls',
        namespace=namespace,
        resource_name=resource_name,
    )
    try:
        r = request_delete(cluster, path)
        r.raise_for_status()
        return True
    except Exception as ex:
        logger.exception("K8s delete request failed")
        return False


def read_stored_clusters(bucket, prefix, delimiter='/'):
    return [
        prefix['Prefix'].split('/')[-2]
        for prefix in these(
            client('s3', endpoint_url=AWS_ENDPOINT).list_objects(
                Bucket=bucket,
                Delimiter=delimiter,
                Prefix=prefix,
            ),
            'CommonPrefixes',
        )
    ]


def read_versions(
    pg_cluster,
    bucket,
    prefix,
    delimiter='/',
):
    return [
        'base' if uid == 'wal' else uid
        for prefix in these(
            client('s3', endpoint_url=AWS_ENDPOINT).list_objects(
                Bucket=bucket,
                Delimiter=delimiter,
                Prefix=prefix + pg_cluster + delimiter,
            ),
            'CommonPrefixes',
        )

        for uid in [prefix['Prefix'].split('/')[-2]]

        if uid == 'wal' or defaulting(lambda: UUID(uid))
    ]

def lsn_to_wal_segment_stop(finish_lsn, start_segment, wal_segment_size=16 * 1024 * 1024):
    timeline = int(start_segment[:8], 16)
    log_id = finish_lsn >> 32
    seg_id = (finish_lsn & 0xFFFFFFFF) // wal_segment_size
    return f"{timeline:08X}{log_id:08X}{seg_id:08X}"

def lsn_to_offset_hex(lsn, wal_segment_size=16 * 1024 * 1024):
    return f"{lsn % wal_segment_size:08X}"

def read_basebackups(
    pg_cluster,
    uid,
    bucket,
    prefix,
    postgresql_versions,
):
    suffix = '' if uid == 'base' else '/' + uid
    backups = []

    for vp in postgresql_versions:
        backup_prefix = f'{prefix}{pg_cluster}{suffix}/wal/{vp}/basebackups_005/'
        logger.info(f"{bucket}/{backup_prefix}")

        paginator = client('s3', endpoint_url=AWS_ENDPOINT).get_paginator('list_objects_v2')
        pages = paginator.paginate(Bucket=bucket, Prefix=backup_prefix)

        for page in pages:
            for obj in page.get("Contents", []):
                key = obj["Key"]
                if not key.endswith("backup_stop_sentinel.json"):
                    continue

                response = client('s3', endpoint_url=AWS_ENDPOINT).get_object(Bucket=bucket, Key=key)
                backup_info = loads(response["Body"].read().decode("utf-8"))
                last_modified = response["LastModified"].astimezone(timezone.utc).isoformat()

                backup_name = key.split("/")[-1].replace("_backup_stop_sentinel.json", "")
                start_seg, start_offset = backup_name.split("_")[1], backup_name.split("_")[-1] if "_" in backup_name else None

                if "LSN" in backup_info and "FinishLSN" in backup_info:
                    # WAL-G
                    lsn = backup_info["LSN"]
                    finish_lsn = backup_info["FinishLSN"]
                    backups.append({
                        "expanded_size_bytes": backup_info.get("UncompressedSize"),
                        "last_modified": last_modified,
                        "name": backup_name,
                        "wal_segment_backup_start": start_seg,
                        "wal_segment_backup_stop": lsn_to_wal_segment_stop(finish_lsn, start_seg),
                        "wal_segment_offset_backup_start": lsn_to_offset_hex(lsn),
                        "wal_segment_offset_backup_stop": lsn_to_offset_hex(finish_lsn),
                    })
                elif "wal_segment_backup_stop" in backup_info:
                    # WAL-E
                    stop_seg = backup_info["wal_segment_backup_stop"]
                    stop_offset = backup_info["wal_segment_offset_backup_stop"]

                    backups.append({
                        "expanded_size_bytes": backup_info.get("expanded_size_bytes"),
                        "last_modified": last_modified,
                        "name": backup_name,
                        "wal_segment_backup_start": start_seg,
                        "wal_segment_backup_stop": stop_seg,
                        "wal_segment_offset_backup_start": start_offset,
                        "wal_segment_offset_backup_stop": stop_offset,
                    })

    return backups


def parse_time(s: str):
    return (
        datetime.strptime(s, '%Y-%m-%dT%H:%M:%SZ')
        .replace(tzinfo=timezone.utc)
        .timestamp()
    )
