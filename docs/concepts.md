# Concepts

## Scope

The scope of the postgres operator is on provisioning, modifying configuration
and cleaning up Postgres clusters that use Patroni, basically to make it easy
and convenient to run Patroni based clusters on Kubernetes. The provisioning
and modifying includes Kubernetes resources on one side but also e.g. database
and role provisioning once the cluster is up and running. We try to leave as
much work as possible to Kubernetes and to Patroni where it fits, especially
the cluster bootstrap and high availability. The operator is however involved
in some overarching orchestration, like rolling updates to improve the user
experience.

Monitoring of clusters is not in scope, for this good tools already exist from
ZMON to Prometheus and more Postgres specific options.

## Status

This project is currently in active development. It is however already
[used internally by Zalando](https://jobs.zalando.com/tech/blog/postgresql-in-a-time-of-kubernetes/)
in order to run Postgres clusters on Kubernetes in larger numbers for staging
environments and a growing number of production clusters. In this environment
the operator is deployed to multiple Kubernetes clusters, where users deploy
manifests via our CI/CD infrastructure or rely on a slim user interface to
create manifests.

Please, report any issues discovered to https://github.com/zalando-incubator/postgres-operator/issues.

## Talks

1. "Blue elephant on-demand: Postgres + Kubernetes" talk by Oleksii Kliukin and Jan Mussler, FOSDEM 2018: [video](https://fosdem.org/2018/schedule/event/blue_elephant_on_demand_postgres_kubernetes/) | [slides (pdf)](https://www.postgresql.eu/events/fosdem2018/sessions/session/1735/slides/59/FOSDEM%202018_%20Blue_Elephant_On_Demand.pdf)

2. "Kube-Native Postgres" talk by Josh Berkus, KubeCon 2017: [video](https://www.youtube.com/watch?v=Zn1vd7sQ_bc)
