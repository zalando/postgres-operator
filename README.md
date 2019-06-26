# Postgres Operator

[![Build Status](https://travis-ci.org/zalando/postgres-operator.svg?branch=master)](https://travis-ci.org/zalando/postgres-operator)
[![Coverage Status](https://coveralls.io/repos/github/zalando/postgres-operator/badge.svg)](https://coveralls.io/github/zalando/postgres-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/zalando/postgres-operator)](https://goreportcard.com/report/github.com/zalando/postgres-operator)
[![GoDoc](https://godoc.org/github.com/zalando/postgres-operator?status.svg)](https://godoc.org/github.com/zalando/postgres-operator)
[![golangci](https://golangci.com/badges/github.com/zalando/postgres-operator.svg)](https://golangci.com/r/github.com/zalando/postgres-operator)

<img src="docs/diagrams/logo.png" width="200">

The Postgres operator manages PostgreSQL clusters on Kubernetes. Configuration
happens only via manifests. Therefore, it integrates easily in automated CI/CD
pipelines with no access to Kubernetes directly.

By default, the operator is building up on two other Open Source projects
developed at Zalando. [Spilo](https://github.com/zalando/spilo) provides the
Docker image that contains PostgreSQL incl. some pre-compiled extensions. Spilo
also includes [Patroni]((https://github.com/zalando/spilo), to manage highly
available Postgres cluster powered by streaming replication.

# Getting started

For a quick first impression follow the instructions of [this](docs/quickstart.md)
tutorial.

# Documentation

There is a browser-friendly version of this documentation at
[postgres-operator.readthedocs.io](https://postgres-operator.readthedocs.io)

* [concepts](docs/index.md)
* [user documentation](docs/user.md)
* [administrator documentation](docs/administrator.md)
* [developer documentation](docs/developer.md)
* [operator configuration reference](docs/reference/operator_parameters.md)
* [cluster manifest reference](docs/reference/cluster_manifest.md)
* [command-line options and environment variables](docs/reference/command_line_and_environment.md)

# Google Summer of Code

The Postgres Operator made it to the [Google Summer of Code 2019](https://summerofcode.withgoogle.com/)!
As a brand new mentoring organization, we are now looking for our first mentees.
Check [our ideas](https://github.com/zalando/postgres-operator/blob/master/docs/gsoc-2019/ideas.md#google-summer-of-code-2019)
and start discussion in [the issue tracker](https://github.com/zalando/postgres-operator/issues).
And don't forget to spread a word about our GSoC participation to attract even
more students.

# Community      

There are two places to get in touch with the community:
1. The [GitHub issue tracker](https://github.com/zalando/postgres-operator/issues)
2. The #postgres-operator slack channel under [Postgres Slack](https://postgres-slack.herokuapp.com)
