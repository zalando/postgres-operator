import logging
from logging.config import dictConfig

dictConfig(
    {
        "version": 1,
        "disable_existing_loggers": True,
        "formatters": {
            "json": {
                "class": "pythonjsonlogger.jsonlogger.JsonFormatter",
                "format": "%(asctime)s %(levelname)s: %(message)s",
            }
        },
        "handlers": {
            "stream_handler": {
                "class": "logging.StreamHandler",
                "formatter": "json",
                "stream": "ext://flask.logging.wsgi_errors_stream",
            }
        },
        "root": {
            "level": "DEBUG",
            "handlers": ["stream_handler"]
        }
    }
)


class Logger:
    def __init__(self):
        self.logger = logging.getLogger(__name__)

    def debug(self, msg: str, *args, **kwargs):
        self.logger.debug(msg, *args, **kwargs)

    def info(self, msg: str, *args, **kwargs):
        self.logger.info(msg, *args, **kwargs)

    def error(self, msg: str, *args, **kwargs):
        self.logger.error(msg, *args, **kwargs)

    def exception(self, msg: str, *args, **kwargs):
        self.logger.exception(msg, *args, **kwargs)


logger = Logger()
