from boto3 import client
from datetime import datetime, timezone
from furl import furl
from json import dumps, loads
from logging import getLogger
from os import environ, getenv
from requests import Session
from urllib.parse import urljoin
from uuid import UUID
from wal_e.cmd import configure_backup_cxt

from .utils import Attrs, defaulting, these


logger = getLogger(__name__)

session = Session()

OPERATOR_CLUSTER_NAME_LABEL = getenv('OPERATOR_CLUSTER_NAME_LABEL', 'cluster-name')

COMMON_CLUSTER_LABEL = getenv('COMMON_CLUSTER_LABEL', '{"application":"spilo"}')
COMMON_POOLER_LABEL = getenv('COMMONG_POOLER_LABEL', '{"application":"db-connection-pooler"}')

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
        label_selector={OPERATOR_CLUSTER_NAME_LABEL: spilo_cluster},
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
            client('s3').list_objects(
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
    s3_endpoint,
    prefix,
    delimiter='/',
    use_aws_instance_profile=False,
):
    return [
        'base' if uid == 'wal' else uid
        for prefix in these(
            client('s3').list_objects(
                Bucket=bucket,
                Delimiter=delimiter,
                Prefix=prefix + pg_cluster + delimiter,
            ),
            'CommonPrefixes',
        )

        for uid in [prefix['Prefix'].split('/')[-2]]

        if uid == 'wal' or defaulting(lambda: UUID(uid))
    ]


def read_basebackups(
    pg_cluster,
    uid,
    bucket,
    s3_endpoint,
    prefix,
    delimiter='/',
    use_aws_instance_profile=False,
):
    environ['WALE_S3_ENDPOINT'] = s3_endpoint
    suffix = '' if uid == 'base' else '/' + uid
    return [
        {
            key: value
            for key, value in basebackup.__dict__.items()
            if isinstance(value, str) or isinstance(value, int)
        }
        for basebackup in Attrs.call(
            f=configure_backup_cxt,
            aws_instance_profile=use_aws_instance_profile,
            s3_prefix=f's3://{bucket}/{prefix}{pg_cluster}{suffix}/wal/',
        )._backup_list(detail=True)._backup_list(prefix=f"{prefix}{pg_cluster}{suffix}/wal/")
    ]


def parse_time(s: str):
    return (
        datetime.strptime(s, '%Y-%m-%dT%H:%M:%SZ')
        .replace(tzinfo=timezone.utc)
        .timestamp()
    )
