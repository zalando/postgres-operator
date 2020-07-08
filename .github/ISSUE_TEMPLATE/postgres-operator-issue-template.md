---
name: Postgres Operator issue template
about: How are you using the operator?
title: ''
labels: ''
assignees: ''

---

Please, answer some short questions which should help us to understand your problem / question better?

- **Which version of the operator are you using?** e.g. v1.5.0
- **Are you running Postgres Operator in production?** [yes | no]
- **Where do you run it? Kubernetes or OpenShift?** [AWS K8s | GCP ... | Bare Metal K8s]
- **Type of issue?** [Bug report, question, feature request, etc.]

Some general remarks when posting a bug report:
- Please, check the operator and postgres pod (Patroni) logs first. When copy pasting many log lines please do it in a separate GitHub gist together with your Postgres CRD and configuration manifest.
- If you feel this issue might be more related to the [Spilo](https://github.com/zalando/spilo/issues) docker image or [Patroni](https://github.com/zalando/patroni/issues), consider opening issues in the respective repos.
