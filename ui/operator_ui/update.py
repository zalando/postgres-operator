import logging
import time

import gevent
import json_delta
import requests.exceptions

from .backoff import expo, random_jitter
from .utils import get_short_error_message

logger = logging.getLogger(__name__)

