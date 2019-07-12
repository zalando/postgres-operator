# Postgres Operator

[![Build Status](https://travis-ci.org/zalando/postgres-operator.svg?branch=master)](https://travis-ci.org/zalando/postgres-operator)
[![Coverage Status](https://coveralls.io/repos/github/zalando/postgres-operator/badge.svg)](https://coveralls.io/github/zalando/postgres-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/zalando/postgres-operator)](https://goreportcard.com/report/github.com/zalando/postgres-operator)
[![GoDoc](https://godoc.org/github.com/zalando/postgres-operator?status.svg)](https://godoc.org/github.com/zalando/postgres-operator)
[![golangci](https://golangci.com/badges/github.com/zalando/postgres-operator.svg)](https://golangci.com/r/github.com/zalando/postgres-operator)

<img src="docs/diagrams/logo.png" width="200">

The Postgres Operator enables highly-available [PostgreSQL](https://www.postgresql.org/)
clusters on Kubernetes (K8s) powered by [Patroni](https://github.com/zalando/spilo).
It is configured only through manifests to ease integration into automated CI/CD
pipelines with no access to Kubernetes directly.

The Postgres Operator has been developed at Zalando and is being used in
production for over two years.

## Getting started

For a quick first impression follow the instructions of this
[tutorial](docs/quickstart.md).

## Documentation

There is a browser-friendly version of this documentation at
[postgres-operator.readthedocs.io](https://postgres-operator.readthedocs.io)

* [How it works](docs/index.md)
* [The Postgres experience on K8s](docs/user.md)
* [The Postgres Operator UI](docs/operator-ui.md)
* [DBA options - from RBAC to backup](docs/administrator.md)
* [Debug and extend the operator](docs/developer.md)
* [Configuration options](docs/reference/operator_parameters.md)
* [Postgres manifest reference](docs/reference/cluster_manifest.md)
* [Command-line options and environment variables](docs/reference/command_line_and_environment.md)

## Google Summer of Code

The Postgres Operator made it to the [Google Summer of Code 2019](https://summerofcode.withgoogle.com/organizations/5429926902104064/)!
Check [our ideas](docs/gsoc-2019/ideas.md#google-summer-of-code-2019)
and start discussions in [the issue tracker](https://github.com/zalando/postgres-operator/issues).

## Community

There are two places to get in touch with the community:
1. The [GitHub issue tracker](https://github.com/zalando/postgres-operator/issues)
2. The #postgres-operator slack channel under [Postgres Slack](https://postgres-slack.herokuapp.com)
