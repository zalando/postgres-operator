import os

from flask_oauthlib.client import OAuthRemoteApp


CREDENTIALS_DIR = os.getenv('CREDENTIALS_DIR', '')


class OAuthRemoteAppWithRefresh(OAuthRemoteApp):
    '''Same as flask_oauthlib.client.OAuthRemoteApp, but always loads client credentials from file.'''

    def __init__(self, oauth, name, **kwargs):
        # constructor expects some values, so make it happy..
        kwargs['consumer_key'] = 'not-needed-here'
        kwargs['consumer_secret'] = 'not-needed-here'
        OAuthRemoteApp.__init__(self, oauth, name, **kwargs)

    def refresh_credentials(self):
        with open(os.path.join(CREDENTIALS_DIR, 'authcode-client-id')) as fd:
            self._consumer_key = fd.read().strip()
        with open(os.path.join(CREDENTIALS_DIR, 'authcode-client-secret')) as fd:
            self._consumer_secret = fd.read().strip()

    @property
    def consumer_key(self):
        self.refresh_credentials()
        return self._consumer_key

    @property
    def consumer_secrect(self):
        self.refresh_credentials()
        return self._consumer_secret
