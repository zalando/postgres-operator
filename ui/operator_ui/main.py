#!/usr/bin/env python3
# pylama:ignore=E402

import gevent.monkey

gevent.monkey.patch_all()

import requests
import tokens
import sys

from backoff import expo, on_exception
from click import ParamType, command, echo, option

from flask import (
    Flask,
    Response,
    abort,
    redirect,
    render_template,
    request,
    send_from_directory,
    session,
)

from flask_oauthlib.client import OAuth
from functools import wraps
from gevent import sleep, spawn
from gevent.pywsgi import WSGIServer
from jq import jq
from json import dumps, loads
from logging import DEBUG, ERROR, INFO, basicConfig, exception, getLogger
from os import getenv
from re import X, compile
from requests.exceptions import RequestException
from signal import SIGTERM, signal
from urllib.parse import urljoin

from . import __version__
from .cluster_discovery import DEFAULT_CLUSTERS, StaticClusterDiscoverer
from .oauth import OAuthRemoteAppWithRefresh

from .spiloutils import (
    apply_postgresql,
    create_postgresql,
    read_basebackups,
    read_namespaces,
    read_pooler,
    read_pods,
    read_postgresql,
    read_postgresqls,
    read_service,
    read_statefulset,
    read_stored_clusters,
    read_versions,
    remove_postgresql,
)

from .utils import (
    const,
    identity,
    these,
)


# Disable access logs from Flask
getLogger('gevent').setLevel(ERROR)

logger = getLogger(__name__)

SERVER_STATUS = {'shutdown': False}

APP_URL = getenv('APP_URL')
AUTHORIZE_URL = getenv('AUTHORIZE_URL')
SPILO_S3_BACKUP_BUCKET = getenv('SPILO_S3_BACKUP_BUCKET')
TEAM_SERVICE_URL = getenv('TEAM_SERVICE_URL')
ACCESS_TOKEN_URL = getenv('ACCESS_TOKEN_URL')
TOKENINFO_URL = getenv('OAUTH2_TOKEN_INFO_URL')

OPERATOR_API_URL = getenv('OPERATOR_API_URL', 'http://postgres-operator')
OPERATOR_CLUSTER_NAME_LABEL = getenv('OPERATOR_CLUSTER_NAME_LABEL', 'cluster-name')
OPERATOR_UI_CONFIG = loads(getenv('OPERATOR_UI_CONFIG', '{}'))
OPERATOR_UI_MAINTENANCE_CHECK = getenv('OPERATOR_UI_MAINTENANCE_CHECK', '{}')
READ_ONLY_MODE = getenv('READ_ONLY_MODE', False) in [True, 'true']
SPILO_S3_BACKUP_PREFIX = getenv('SPILO_S3_BACKUP_PREFIX', 'spilo/')
SUPERUSER_TEAM = getenv('SUPERUSER_TEAM', 'acid')
TARGET_NAMESPACE = getenv('TARGET_NAMESPACE')
GOOGLE_ANALYTICS = getenv('GOOGLE_ANALYTICS', False)
MIN_PODS= getenv('MIN_PODS', 2)
RESOURCES_VISIBLE = getenv('RESOURCES_VISIBLE', True)
CUSTOM_MESSAGE_RED = getenv('CUSTOM_MESSAGE_RED', '')

APPLICATION_DEPLOYMENT_DOCS = getenv('APPLICATION_DEPLOYMENT_DOCS', '')
CONNECTION_DOCS = getenv('CONNECTION_DOCS', '')

# storage pricing, i.e. https://aws.amazon.com/ebs/pricing/ (e.g. Europe - Franfurt)
COST_EBS = float(getenv('COST_EBS', 0.0952))  # GB per month
COST_IOPS = float(getenv('COST_IOPS', 0.006))  # IOPS per month above 3000 baseline
COST_THROUGHPUT = float(getenv('COST_THROUGHPUT', 0.0476))  # MB/s per month above 125 MB/s baseline

# compute costs, i.e. https://www.ec2instances.info/?region=eu-central-1&selected=m5.2xlarge
COST_CORE = float(getenv('COST_CORE', 0.0575))  # Core per hour m5.2xlarge / 8.
COST_MEMORY = float(getenv('COST_MEMORY', 0.014375))  # Memory GB m5.2xlarge / 32.
COST_ELB = float(getenv('COST_ELB', 0.03))     # per hour

# maximum and limitation of IOPS and throughput 
FREE_IOPS = float(getenv('FREE_IOPS', 3000)) 
LIMIT_IOPS = float(getenv('LIMIT_IOPS', 16000))
FREE_THROUGHPUT = float(getenv('FREE_THROUGHPUT', 125))
LIMIT_THROUGHPUT = float(getenv('LIMIT_THROUGHPUT', 1000))
# get the default value of core and memory
DEFAULT_MEMORY = getenv('DEFAULT_MEMORY', '300Mi')
DEFAULT_MEMORY_LIMIT = getenv('DEFAULT_MEMORY_LIMIT', '300Mi')
DEFAULT_CPU = getenv('DEFAULT_CPU', '10m')
DEFAULT_CPU_LIMIT = getenv('DEFAULT_CPU_LIMIT', '300m')

WALE_S3_ENDPOINT = getenv(
    'WALE_S3_ENDPOINT',
    'https+path://s3.eu-central-1.amazonaws.com:443',
)

USE_AWS_INSTANCE_PROFILE = (
    getenv('USE_AWS_INSTANCE_PROFILE', 'false').lower() != 'false'
)

AWS_ENDPOINT = getenv('AWS_ENDPOINT')

tokens.configure()
tokens.manage('read-only')
tokens.start()


def service_auth_header():
    token = getenv('SERVICE_TOKEN') or tokens.get('read-only')
    return {
        'Authorization': f'Bearer {token}',
    }


MAX_CONTENT_LENGTH = 16 * 1024 * 1024
app = Flask(__name__)
app.config['MAX_CONTENT_LENGTH'] = MAX_CONTENT_LENGTH


class WSGITransferEncodingChunked:
    """Support HTTP Transfer-Encoding: chunked transfers"""

    def __init__(self, app):
        self.app = app

    def __call__(self, environ, start_response):
        from io import BytesIO
        input = environ.get('wsgi.input')
        length = environ.get('CONTENT_LENGTH', '0')
        length = 0 if length == '' else int(length)
        body = b''
        if length == 0:
            if input is None:
                return
            if environ.get('HTTP_TRANSFER_ENCODING', '0') == 'chunked':
                size = int(input.readline(), 16)
                total_size = 0
                while size > 0:
                    # Validate max size to avoid DoS attacks
                    total_size += size
                    if total_size > MAX_CONTENT_LENGTH:
                        # Avoid DoS (using all available memory by streaming an
                        # infinite file)
                        start_response(
                            '413 Request Entity Too Large',
                            [('Content-Type', 'text/plain')],
                        )
                        return []

                    body += input.read(size + 2)
                    size = int(input.readline(), 16)

        else:
            body = environ['wsgi.input'].read(length)

        environ['CONTENT_LENGTH'] = str(len(body))
        environ['wsgi.input'] = BytesIO(body)

        return self.app(environ, start_response)


oauth = OAuth(app)

auth = OAuthRemoteAppWithRefresh(
    oauth,
    'auth',
    request_token_url=None,
    access_token_method='POST',
    access_token_url=ACCESS_TOKEN_URL,
    authorize_url=AUTHORIZE_URL,
)
oauth.remote_apps['auth'] = auth


def verify_token(token):
    if not token:
        return False

    r = requests.get(TOKENINFO_URL, headers={'Authorization': token})

    return r.status_code == 200


def authorize(f):
    @wraps(f)
    def wrapper(*args, **kwargs):
        if AUTHORIZE_URL and 'auth_token' not in session:
            return redirect(urljoin(APP_URL, '/login'))
        return f(*args, **kwargs)

    return wrapper


def ok(body={}, status=200):
    return (
        Response(
            (
                dumps(body)
                if isinstance(body, dict) or isinstance(body, list)
                else body
            ),
            mimetype='application/json',
        ),
        status
    )


def fail(body={}, status=400, **kwargs):
    return (
        Response(
            dumps(
                {
                    'error': ' '.join(body.split()).format(**kwargs),
                }
                if isinstance(body, str)
                else body,
            ),
            mimetype='application/json',
        ),
        status,
    )


def not_found(body={}, **kwargs):
    return fail(body=body, status=404, **kwargs)


def respond(data, f=identity):
    return (
        ok(f(data))
        if data is not None
        else not_found()
    )


def wrong_namespace(**kwargs):
    return fail(
        body=f'The Kubernetes namespace must be {TARGET_NAMESPACE}',
        status=403,
        **kwargs
    )


def no_writes_when_read_only(**kwargs):
    return fail(
        body='UI is in read-only mode for production',
        status=403,
        **kwargs
    )


@app.route('/health')
def health():
    if SERVER_STATUS['shutdown']:
        abort(503)
    else:
        return 'OK'


STATIC_HEADERS = {
    'cache-control': ', '.join([
        'no-store',
        'no-cache',
        'must-revalidate',
        'post-check=0',
        'pre-check=0',
        'max-age=0',
    ]),
    'Pragma': 'no-cache',
    'Expires': '-1',
}


@app.route('/css/<path:path>')
@authorize
def send_css(path):
    return send_from_directory('static/', path), 200, STATIC_HEADERS


@app.route('/js/<path:path>')
@authorize
def send_js(path):
    return send_from_directory('static/', path), 200, STATIC_HEADERS


@app.route('/')
@authorize
def index():
    return render_template('index.html', google_analytics=GOOGLE_ANALYTICS, app_url=APP_URL)


DEFAULT_UI_CONFIG = {
    'docs_link': 'https://github.com/zalando/postgres-operator',
    'odd_host_visible': True,
    'nat_gateways_visible': True,
    'users_visible': True,
    'databases_visible': True,
    'resources_visible': RESOURCES_VISIBLE,
    'postgresql_versions': ['11', '12', '13', '14', '15'],
    'dns_format_string': '{0}.{1}',
    'pgui_link': '',
    'static_network_whitelist': {},
    'read_only_mode': READ_ONLY_MODE,
    'superuser_team': SUPERUSER_TEAM,
    'target_namespace': TARGET_NAMESPACE,
    'connection_docs': CONNECTION_DOCS,
    'application_deployment_docs': APPLICATION_DEPLOYMENT_DOCS,
    'cost_ebs': COST_EBS,
    'cost_iops': COST_IOPS,
    'cost_throughput': COST_THROUGHPUT,
    'cost_core': COST_CORE,
    'cost_memory': COST_MEMORY,
    'cost_elb': COST_ELB,
    'min_pods': MIN_PODS,
    'free_iops': FREE_IOPS, 
    'free_throughput': FREE_THROUGHPUT,
    'limit_iops': LIMIT_IOPS,
    'limit_throughput': LIMIT_THROUGHPUT
}


@app.route('/config')
@authorize
def get_config():
    config = DEFAULT_UI_CONFIG.copy() 
    config.update(OPERATOR_UI_CONFIG)

    config['namespaces'] = (
        [TARGET_NAMESPACE]
        if TARGET_NAMESPACE not in ['', '*']
        else [
            namespace_name
            for namespace in these(
                read_namespaces(get_cluster()),
                'items',
            )
            for namespace_name in [namespace['metadata']['name']]
            if namespace_name not in [
                'kube-public',
                'kube-system',
            ]
        ]
    )

    try:

        kubernetes_maintenance_check = (
            config.get('kubernetes_maintenance_check') or
            loads(OPERATOR_UI_MAINTENANCE_CHECK)
        )

        if (
            kubernetes_maintenance_check and
            {'url', 'query'} <= kubernetes_maintenance_check.keys()
        ):
            config['kubernetes_in_maintenance'] = (
                jq(kubernetes_maintenance_check['query']).transform(
                    requests.get(
                        kubernetes_maintenance_check['url'],
                        headers=service_auth_header(),
                    ).json(),
                )
            )

    except ValueError:
        exception('Could not determine Kubernetes cluster status')

    return ok(config)


def get_teams_for_user(user_name):
    if not TEAM_SERVICE_URL:
        return loads(getenv('TEAMS', '[]'))

    return [
        team['id'].lower()
        for team in requests.get(
            TEAM_SERVICE_URL.format(user_name),
            headers=service_auth_header(),
        ).json()
    ]


@app.route('/teams')
@authorize
def get_teams():
    return ok(
        get_teams_for_user(
            session.get('user_name', ''),
        )
    )


@app.route('/services/<namespace>/<cluster>')
@authorize
def get_service(namespace: str, cluster: str):

    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    return respond(
        read_service(
            get_cluster(),
            namespace,
            cluster,
        ),
    )


@app.route('/pooler/<namespace>/<cluster>')
@authorize
def get_list_poolers(namespace: str, cluster: str):

    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    return respond(
        read_pooler(
            get_cluster(),
            namespace,
            "{}-pooler".format(cluster),
        ),
    )


@app.route('/statefulsets/<namespace>/<cluster>')
@authorize
def get_list_clusters(namespace: str, cluster: str):

    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    return respond(
        read_statefulset(
            get_cluster(),
            namespace,
            cluster,
        ),
    )


@app.route('/statefulsets/<namespace>/<cluster>/pods')
@authorize
def get_list_members(namespace: str, cluster: str):

    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    return respond(
        read_pods(
            get_cluster(),
            namespace,
            cluster,
        ),
        lambda pods: [
            pod['metadata']
            for pod in these(pods, 'items')
        ],
    )


@app.route('/namespaces')
@authorize
def get_namespaces():

    if TARGET_NAMESPACE not in ['', '*']:
        return ok([TARGET_NAMESPACE])

    return respond(
        read_namespaces(
            get_cluster(),
        ),
        lambda namespaces: [
            namespace['name']
            for namespace in these(namespaces, 'items')
        ],
    )


@app.route('/postgresqls')
@authorize
def get_postgresqls():
    postgresqls = [
        {
            'nodes': spec.get('numberOfInstances', ''),
            'memory': spec.get('resources', {}).get('requests', {}).get('memory', OPERATOR_UI_CONFIG.get("default_memory", DEFAULT_MEMORY)),
            'memory_limit': spec.get('resources', {}).get('limits', {}).get('memory', OPERATOR_UI_CONFIG.get("default_memory_limit", DEFAULT_MEMORY_LIMIT)),
            'cpu': spec.get('resources', {}).get('requests', {}).get('cpu', OPERATOR_UI_CONFIG.get("default_cpu", DEFAULT_CPU)),
            'cpu_limit': spec.get('resources', {}).get('limits', {}).get('cpu', OPERATOR_UI_CONFIG.get("default_cpu_limit", DEFAULT_CPU_LIMIT)),
            'volume_size': spec.get('volume', {}).get('size', 0),
            'iops': spec.get('volume', {}).get('iops', 3000),
            'throughput': spec.get('volume', {}).get('throughput', 125),
            'team': (
                spec.get('teamId') or
                metadata.get('labels', {}).get('team', '')
            ),
            'namespace': namespace,
            'name': name,
            'uid': uid,
            'namespaced_name': namespace + '/' + name,
            'full_name': namespace + '/' + name + ('/' + uid if uid else ''),
            'status': status,
            'num_elb': spec.get('enableMasterLoadBalancer', 0) + spec.get('enableReplicaLoadBalancer', 0) + \
                       spec.get('enableMasterPoolerLoadBalancer', 0) + spec.get('enableReplicaPoolerLoadBalancer', 0),
        }
        for cluster in these(
            read_postgresqls(
                get_cluster(),
                namespace=(
                    None
                    if TARGET_NAMESPACE in ['', '*']
                    else TARGET_NAMESPACE
                ),
            ),
            'items',
        )
        for spec in [cluster.get('spec', {}) if cluster.get('spec', {}) is not None else {"error": "Invalid spec in manifest"}]
        for status in [cluster.get('status', {})]
        for metadata in [cluster['metadata']]
        for namespace in [metadata['namespace']]
        for name in [metadata['name']]
        for uid in [metadata.get('uid', '')]
    ]
    return respond(postgresqls)


# Note these are meant to be consistent with the operator backend validations;
# See https://github.com/zalando/postgres-operator/blob/master/pkg/cluster/cluster.go  # noqa
VALID_SIZE = compile(r'^[1-9][0-9]{0,3}Gi$')
VALID_CLUSTER_NAME = compile(r'^[a-z0-9]+[a-z0-9\-]+[a-z0-9]+$')
VALID_DATABASE_NAME = compile(r'^[a-zA-Z_][a-zA-Z0-9_]*$')
VALID_USERNAME = compile(
    r'''
        ^[a-z0-9]([-_a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-_a-z0-9]*[a-z0-9])?)*$
    ''',
    X,
)

ROLEFLAGS = '''
    SUPERUSER
    INHERIT
    LOGIN
    NOLOGIN
    CREATEROLE
    CREATEDB
    REPLICATION
    BYPASSRLS
'''.split()


def namespaced(handler):

    def run(*args, **kwargs):
        namespace = (
            args[1]
            if len(args) >= 2
            else kwargs['namespace']
        )

        if TARGET_NAMESPACE not in ['', '*', namespace]:
            return wrong_namespace()

        return handler(*args, **kwargs)

    return run


def read_only(handler):

    def run(*args, **kwargs):
        if READ_ONLY_MODE:
            return no_writes_when_read_only()

        return handler(*args, **kwargs)

    return run


@app.route('/postgresqls/<namespace>/<cluster>', methods=['POST'])
@authorize
@namespaced
def update_postgresql(namespace: str, cluster: str):
    if READ_ONLY_MODE:
        return no_writes_when_read_only()

    o = read_postgresql(get_cluster(), namespace, cluster)
    if o is None:
        return not_found()

    postgresql = request.get_json(force=True)

    teams = get_teams_for_user(session.get('user_name', ''))
    logger.info(f'Changes to: {cluster} by {session.get("user_name", "local-user")}/{teams} {postgresql}')  # noqa

    if SUPERUSER_TEAM and SUPERUSER_TEAM in teams:
        logger.info(f'Allowing edit due to membership in superuser team {SUPERUSER_TEAM}')  # noqa
    elif not o['spec']['teamId'].lower() in teams:
        return fail('Not a member of the owning team', status=401)

    # do spec copy 1 by 1 not to do unsupporeted changes for now
    spec = {}
    if 'allowedSourceRanges' in postgresql['spec']:
        if not isinstance(postgresql['spec']['allowedSourceRanges'], list):
            return fail('allowedSourceRanges invalid')
        spec['allowedSourceRanges'] = postgresql['spec']['allowedSourceRanges']

    if 'numberOfInstances' in postgresql['spec']:
        if not isinstance(postgresql['spec']['numberOfInstances'], int):
            return fail('numberOfInstances invalid')
        spec['numberOfInstances'] = postgresql['spec']['numberOfInstances']

    if (
        'volume' in postgresql['spec']
        and 'size' in postgresql['spec']['volume']
    ):
        size = str(postgresql['spec']['volume']['size'])
        if not VALID_SIZE.match(size):
            return fail('volume.size is invalid; should be like 123Gi')

        spec['volume'] = {'size': size}

    if (
        'volume' in postgresql['spec']
        and 'iops' in postgresql['spec']['volume']
        and postgresql['spec']['volume']['iops'] != None
    ):
        iops = int(postgresql['spec']['volume']['iops'])
        if not 'volume' in spec:
            spec['volume'] = {}

        spec['volume']['iops'] = iops

    if (
        'volume' in postgresql['spec']
        and 'throughput' in postgresql['spec']['volume']
        and postgresql['spec']['volume']['throughput'] != None
    ):
        throughput = int(postgresql['spec']['volume']['throughput'])
        if not 'volume' in spec:
            spec['volume'] = {}

        spec['volume']['throughput'] = throughput

    additional_specs = ['enableMasterLoadBalancer',
                        'enableReplicaLoadBalancer',
                        'enableConnectionPooler',
                        'enableReplicaConnectionPooler',
                        'enableMasterPoolerLoadBalancer',
                        'enableReplicaPoolerLoadBalancer',
                        ]

    for var in additional_specs:
        if postgresql['spec'].get(var):
            spec[var] = True
        else:
            if var in o['spec']:
                del o['spec'][var]

    if 'users' in postgresql['spec']:
        spec['users'] = postgresql['spec']['users']

        if not isinstance(postgresql['spec']['users'], dict):
            return fail('''
                the "users" key must hold a key-value object mapping usernames
                to a list of their role flags as simple strings.
                e.g.: {{"some_username": ["createdb", "login"]}}
            ''')

        for username, role_flags in postgresql['spec']['users'].items():

            if not VALID_USERNAME.match(username):
                return fail(
                    '''
                        no match for valid username pattern {VALID_USERNAME} in
                        invalid username {username}
                    ''',
                    VALID_USERNAME=VALID_USERNAME.pattern,
                    username=username,
                )

            if not isinstance(role_flags, list):
                return fail(
                    '''
                        the value for the user key {username} must be a list of
                        the user's permissions as simple strings.
                        e.g.: ["createdb", "login"]
                    ''',
                    username=username,
                )

            for role_flag in role_flags:

                if not isinstance(role_flag, str):
                    return fail(
                        '''
                            the value for the user key {username} must be a
                            list of the user's permissions as simple strings.
                            e.g.: ["createdb", "login"]
                        ''',
                        username=username,
                    )

                if role_flag.upper() not in ROLEFLAGS:
                    return fail(
                        '''
                            user {username} has invalid role flag {role_flag}
                            - allowed flags are {all_flags}
                        ''',
                        username=username,
                        role_flag=role_flag,
                        all_flags=', '.join(ROLEFLAGS),
                    )

    if 'databases' in postgresql['spec']:
        spec['databases'] = postgresql['spec']['databases']

        if not isinstance(postgresql['spec']['databases'], dict):
            return fail('''
                the "databases" key must hold a key-value object mapping
                database names to their respective owner's username as a simple
                string.  e.g. {{"some_database_name": "some_username"}}
            ''')

        for database_name, owner_username in (
            postgresql['spec']['databases'].items()
        ):

            if not VALID_DATABASE_NAME.match(database_name):
                return fail(
                    '''
                        no match for valid database name pattern
                        {VALID_DATABASE_NAME} in invalid database name
                        {database_name}
                    ''',
                    VALID_DATABASE_NAME=VALID_DATABASE_NAME.pattern,
                    database_name=database_name,
                )

            if not isinstance(owner_username, str):
                return fail(
                    '''
                        the value for the database key {database_name} must be
                        the owning user's username as a simple string.  e.g.:
                        "some_username"
                    ''',
                    database_name=database_name,
                )

            if not VALID_USERNAME.match(owner_username):
                return fail(
                    '''
                        no match for valid username pattern {VALID_USERNAME} in
                        invalid database owner username {owner_username}
                    ''',
                    VALID_USERNAME=VALID_USERNAME.pattern,
                    owner_username=owner_username,
                )

    resource_types = ["cpu","memory"]
    resource_constraints = ["requests","limits"]
    if "resources" in postgresql["spec"]:
        spec["resources"] = {}

        res = postgresql["spec"]["resources"]
        for rt in resource_types:
            for rc in resource_constraints:
                if rc in res:
                    if rt in res[rc]:
                        if not rc in spec["resources"]:
                            spec["resources"][rc] = {}
                        spec["resources"][rc][rt] = res[rc][rt]

    if "postgresql" in postgresql["spec"]:
        if "version" in postgresql["spec"]["postgresql"]:
            if "postgresql" not in spec:
                spec["postgresql"]={}

            spec["postgresql"]["version"] = postgresql["spec"]["postgresql"]["version"]

    o['spec'].update(spec)

    apply_postgresql(get_cluster(), namespace, cluster, o)

    return ok(o)


@app.route('/postgresqls/<namespace>/<cluster>', methods=['GET'])
@authorize
def get_postgresql(namespace: str, cluster: str):

    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    return respond(
        read_postgresql(
            get_cluster(),
            namespace,
            cluster,
        ),
    )


@app.route('/stored_clusters')
@authorize
def get_stored_clusters():
    return respond(
        read_stored_clusters(
            bucket=SPILO_S3_BACKUP_BUCKET,
            prefix=SPILO_S3_BACKUP_PREFIX,
        )
    )


@app.route('/stored_clusters/<pg_cluster>', methods=['GET'])
@authorize
def get_versions(pg_cluster: str):
    return respond(
        read_versions(
            bucket=SPILO_S3_BACKUP_BUCKET,
            pg_cluster=pg_cluster,
            prefix=SPILO_S3_BACKUP_PREFIX,
            s3_endpoint=WALE_S3_ENDPOINT,
            use_aws_instance_profile=USE_AWS_INSTANCE_PROFILE,
        ),
    )



@app.route('/stored_clusters/<pg_cluster>/<uid>', methods=['GET'])
@authorize
def get_basebackups(pg_cluster: str, uid: str):
    return respond(
        read_basebackups(
            bucket=SPILO_S3_BACKUP_BUCKET,
            pg_cluster=pg_cluster,
            prefix=SPILO_S3_BACKUP_PREFIX,
            s3_endpoint=WALE_S3_ENDPOINT,
            uid=uid,
            use_aws_instance_profile=USE_AWS_INSTANCE_PROFILE,
        ),
    )


@app.route('/create-cluster', methods=['POST'])
@authorize
def create_new_cluster():

    if READ_ONLY_MODE:
        return no_writes_when_read_only()

    postgresql = request.get_json(force=True)

    cluster_name = postgresql['metadata']['name']
    if not VALID_CLUSTER_NAME.match(cluster_name):
        return fail(r'metadata.name is invalid. [a-z0-9\-]+')

    namespace = postgresql['metadata']['namespace']
    if not namespace:
        return fail('metadata.namespace must not be empty')
    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    teams = get_teams_for_user(session.get('user_name', ''))
    logger.info(f'Create cluster by {session.get("user_name", "local-user")}/{teams} {postgresql}')  # noqa

    if SUPERUSER_TEAM and SUPERUSER_TEAM in teams:
        logger.info(f'Allowing create due to membership in superuser team {SUPERUSER_TEAM}')  # noqa
    elif not postgresql['spec']['teamId'].lower() in teams:
        return fail('Not a member of the owning team', status=401)

    r = create_postgresql(get_cluster(), namespace, postgresql)
    return ok() if r else fail(status=500)


@app.route('/postgresqls/<namespace>/<cluster>', methods=['DELETE'])
@authorize
def delete_postgresql(namespace: str, cluster: str):
    if TARGET_NAMESPACE not in ['', '*', namespace]:
        return wrong_namespace()

    if READ_ONLY_MODE:
        return no_writes_when_read_only()

    postgresql = read_postgresql(get_cluster(), namespace, cluster)
    if postgresql is None:
        return not_found()

    teams = get_teams_for_user(session.get('user_name', ''))

    logger.info(f'Delete cluster: {cluster} by {session.get("user_name", "local-user")}/{teams}')  # noqa

    if SUPERUSER_TEAM and SUPERUSER_TEAM in teams:
        logger.info(f'Allowing delete due to membership in superuser team {SUPERUSER_TEAM}')  # noqa
    elif not postgresql['spec']['teamId'].lower() in teams:
        return fail('Not a member of the owning team', status=401)

    return respond(
        remove_postgresql(
            get_cluster(),
            namespace,
            cluster,
        ),
        const(None),
    )


def proxy_operator(url: str):
    response = requests.get(OPERATOR_API_URL + url)
    response.raise_for_status()
    return respond(response.json())


@app.route('/operator/status')
@authorize
def get_operator_status():
    return proxy_operator('/status/')


@app.route('/operator/workers/<worker>/queue')
@authorize
def get_operator_get_queue(worker: int):
    return proxy_operator(f'/workers/{worker}/queue')


@app.route('/operator/workers/<worker>/logs')
@authorize
def get_operator_get_logs(worker: int):
    return proxy_operator(f'/workers/{worker}/logs')


@app.route('/operator/clusters/<namespace>/<cluster>/logs')
@authorize
def get_operator_get_logs_per_cluster(namespace: str, cluster: str):
    return proxy_operator(f'/clusters/{namespace}/{cluster}/logs/')


@app.route('/login')
def login():
    redirect = request.args.get('redirect', False)
    if not redirect:
        return render_template('login-deeplink.html')

    redirect_uri = urljoin(APP_URL, '/login/authorized')
    return auth.authorize(callback=redirect_uri)


@app.route('/logout')
def logout():
    session.pop('auth_token', None)
    return redirect(urljoin(APP_URL, '/'))


@app.route('/favicon.png')
def favicon():
    return send_from_directory('static/', 'favicon-96x96.png'), 200


@app.route('/login/authorized')
def authorized():
    resp = auth.authorized_response()
    if resp is None:
        return 'Access denied: reason=%s error=%s' % (
            request.args['error'],
            request.args['error_description']
        )

    if not isinstance(resp, dict):
        return 'Invalid auth response'

    session['auth_token'] = (resp['access_token'], '')

    r = requests.get(
        TOKENINFO_URL,
        headers={
            'Authorization': f'Bearer {session["auth_token"][0]}',
        },
    )
    session['user_name'] = r.json().get('uid')

    logger.info(f'Login from: {session["user_name"]}')

    # return redirect(urljoin(APP_URL, '/'))
    return render_template('login-resolve-deeplink.html')


def shutdown():
    # just wait some time to give Kubernetes time to update endpoints
    # this requires changing the readinessProbe's
    # PeriodSeconds and FailureThreshold appropriately
    # see https://godoc.org/k8s.io/kubernetes/pkg/api/v1#Probe
    sleep(10)
    exit(0)


def exit_gracefully(signum, frame):
    logger.info('Received TERM signal, shutting down..')
    SERVER_STATUS['shutdown'] = True
    spawn(shutdown)


def print_version(ctx, param, value):
    if not value or ctx.resilient_parsing:
        return
    echo(f'PostgreSQL Operator UI {__version__}')
    ctx.exit()


class CommaSeparatedValues(ParamType):
    name = 'comma_separated_values'

    def convert(self, value, param, ctx):
        return (
            filter(None, value.split(','))
            if isinstance(value, str)
            else value
        )


CLUSTER = None


def get_cluster():
    return CLUSTER


def set_cluster(c):
    global CLUSTER
    CLUSTER = c
    return CLUSTER


def init_cluster():
    discoverer = StaticClusterDiscoverer([])
    set_cluster(discoverer.get_clusters()[0])


@command(context_settings={'help_option_names': ['-h', '--help']})
@option(
    '-V',
    '--version',
    callback=print_version,
    expose_value=False,
    help='Print the current version number and exit.',
    is_eager=True,
    is_flag=True,
)
@option(
    '-p',
    '--port',
    default=8081,
    envvar='SERVER_PORT',
    help='HTTP port to listen on (default: 8081)',
    type=int,
)
@option(
    '-d',
    '--debug',
    help='Verbose logging',
    is_flag=True,
)
@option(
    '--secret-key',
    default='development',
    envvar='SECRET_KEY',
    help='Secret key for session cookies',
)
@option(
    '--clusters',
    envvar='CLUSTERS',
    help=f'Comma separated list of Kubernetes API server URLs (default: {DEFAULT_CLUSTERS})',  # noqa
    type=CommaSeparatedValues(),
)
def main(port, secret_key, debug, clusters: list):
    global TARGET_NAMESPACE

    basicConfig(stream=sys.stdout, level=(DEBUG if debug else INFO), format='%(asctime)s %(levelname)s: %(message)s',)

    init_cluster()

    logger.info(f'Access token URL: {ACCESS_TOKEN_URL}')
    logger.info(f'App URL: {APP_URL}')
    logger.info(f'Authorize URL: {AUTHORIZE_URL}')
    logger.info(f'Operator API URL: {OPERATOR_API_URL}')
    logger.info(f'Operator cluster name label: {OPERATOR_CLUSTER_NAME_LABEL}')
    logger.info(f'Readonly mode: {"enabled" if READ_ONLY_MODE else "disabled"}')  # noqa
    logger.info(f'Spilo S3 backup bucket: {SPILO_S3_BACKUP_BUCKET}')
    logger.info(f'Spilo S3 backup prefix: {SPILO_S3_BACKUP_PREFIX}')
    logger.info(f'Superuser team: {SUPERUSER_TEAM}')
    logger.info(f'Target namespace: {TARGET_NAMESPACE}')
    logger.info(f'Teamservice URL: {TEAM_SERVICE_URL}')
    logger.info(f'Tokeninfo URL: {TOKENINFO_URL}')
    logger.info(f'Use AWS instance_profile: {USE_AWS_INSTANCE_PROFILE}')
    logger.info(f'WAL-E S3 endpoint: {WALE_S3_ENDPOINT}')
    logger.info(f'AWS S3 endpoint: {AWS_ENDPOINT}')

    if TARGET_NAMESPACE is None:
        @on_exception(
            expo,
            RequestException,
        )
        def get_target_namespace():
            logger.info('Fetching target namespace from Operator API')
            return (
                requests
                .get(OPERATOR_API_URL + '/config/')
                .json()
                ['operator']
                ['WatchedNamespace']
            )
        TARGET_NAMESPACE = get_target_namespace()
        logger.info(f'Target namespace set to: {TARGET_NAMESPACE or "*"}')

    app.debug = debug
    app.secret_key = secret_key

    signal(SIGTERM, exit_gracefully)

    app.wsgi_app = WSGITransferEncodingChunked(app.wsgi_app)
    http_server = WSGIServer(('0.0.0.0', port), app)
    logger.info(f'Listening on :{port}')
    http_server.serve_forever()
