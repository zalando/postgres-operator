---
name: Postgres Operator issue template
about: How are you using the operator?
title: ''
labels: ''
assignees: ''

---

Please, answer some short questions which should help us to understand your problem / question better?

- **Which image of the operator are you using?** e.g. registry.opensource.zalan.do/acid/postgres-operator:v1.11.0
- **Where do you run it - cloud or metal? Kubernetes or OpenShift?** [AWS K8s | GCP ... | Bare Metal K8s]
- **Are you running Postgres Operator in production?** [yes | no]
- **Type of issue?** [Bug report, question, feature request, etc.]

Some general remarks when posting a bug report:
- Please, check the operator, pod (Patroni) and postgresql logs first. When copy-pasting many log lines please do it in a separate GitHub gist together with your Postgres CRD and configuration manifest.
- If you feel this issue might be more related to the [Spilo](https://github.com/zalando/spilo/issues) docker image or [Patroni](https://github.com/zalando/patroni/issues), consider opening issues in the respective repos.
