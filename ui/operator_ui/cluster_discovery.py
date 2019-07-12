import logging
import re
from pathlib import Path

import kubernetes.client
import kubernetes.config
import tokens
from requests.auth import AuthBase

# default URL points to kubectl proxy
DEFAULT_CLUSTERS = 'http://localhost:8001/'
CLUSTER_ID_INVALID_CHARS = re.compile('[^a-z0-9:-]')

logger = logging.getLogger(__name__)

tokens.configure(from_file_only=True)


def generate_cluster_id(url: str):
    '''Generate some "cluster ID" from given API server URL'''
    for prefix in ('https://', 'http://'):
        if url.startswith(prefix):
            url = url[len(prefix):]
    return CLUSTER_ID_INVALID_CHARS.sub('-', url.lower()).strip('-')


class StaticAuthorizationHeaderAuth(AuthBase):
    '''Static authentication with given "Authorization" header'''

    def __init__(self, authorization):
        self.authorization = authorization

    def __call__(self, request):
        request.headers['Authorization'] = self.authorization
        return request


class OAuthTokenAuth(AuthBase):
    '''Dynamic authentication using the "tokens" library to load OAuth tokens from file
    (potentially mounted from a Kubernetes secret)'''

    def __init__(self, token_name):
        self.token_name = token_name
        tokens.manage(token_name)

    def __call__(self, request):
        token = tokens.get(self.token_name)
        request.headers['Authorization'] = 'Bearer {}'.format(token)
        return request


class Cluster:
    def __init__(self, id, api_server_url, ssl_ca_cert=None, auth=None, cert_file=None, key_file=None):
        self.id = id
        self.api_server_url = api_server_url
        self.ssl_ca_cert = ssl_ca_cert
        self.auth = auth
        self.cert_file = cert_file
        self.key_file = key_file


class StaticClusterDiscoverer:

    def __init__(self, api_server_urls: list):
        self._clusters = []

        if not api_server_urls:
            try:
                kubernetes.config.load_incluster_config()
            except kubernetes.config.ConfigException:
                # we are not running inside a cluster
                # => assume default kubectl proxy URL
                cluster = Cluster(generate_cluster_id(DEFAULT_CLUSTERS), DEFAULT_CLUSTERS)
            else:
                logger.info("in cluster configuration failed")
                config = kubernetes.client.configuration
                cluster = Cluster(
                    generate_cluster_id(config.host),
                    config.host,
                    ssl_ca_cert=config.ssl_ca_cert,
                    auth=StaticAuthorizationHeaderAuth(config.api_key['authorization']))
            self._clusters.append(cluster)
        else:
            for api_server_url in api_server_urls:
                logger.info("api server url: {}".format(api_server_url))
                if 'localhost' not in api_server_url:
                    # TODO: hacky way of detecting whether we need a token or not
                    auth = OAuthTokenAuth('read-only')
                else:
                    auth = None
                self._clusters.append(Cluster(generate_cluster_id(api_server_url), api_server_url, auth=auth))

    def get_clusters(self):
        return self._clusters


class KubeconfigDiscoverer:

    def __init__(self, kubeconfig_path: Path, contexts: set):
        self._path = kubeconfig_path
        self._contexts = contexts

    def get_clusters(self):
        # Kubernetes Python client expects "vintage" string path
        config_file = str(self._path)
        contexts, current_context = kubernetes.config.list_kube_config_contexts(config_file)
        for context in contexts:
            if self._contexts and context['name'] not in self._contexts:
                # filter out
                continue
            config = kubernetes.client.ConfigurationObject()
            kubernetes.config.load_kube_config(config_file, context=context['name'], client_configuration=config)
            authorization = config.api_key.get('authorization')
            if authorization:
                auth = StaticAuthorizationHeaderAuth(authorization)
            else:
                auth = None
            cluster = Cluster(
                context['name'],
                config.host,
                ssl_ca_cert=config.ssl_ca_cert,
                cert_file=config.cert_file,
                key_file=config.key_file,
                auth=auth)
            yield cluster
