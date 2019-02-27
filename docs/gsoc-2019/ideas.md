
# Google Summer of Code 2019

## Applications steps 

1. Please carefully read the official [Google Summer of Code Student Guide](https://google.github.io/gsocguides/student/) 
2. Join the #postgres-operator slack channel under [Postgres Slack](https://postgres-slack.herokuapp.com) to introduce yourself to the community and get quick feedback on your application.
3. Select a project from the list of ideas below or propose your own.
4. Write a proposal draft.  Please open an issue with the label `gsoc2019_application` in the [operator repository](https://github.com/zalando/postgres-operator/issues)  so that the community members can publicly review it. See proposal instructions below for details.
5. Submit proposal and the proof of enrollment before April 9 2019 18:00 UTC through the web site of the Program.

## Project ideas 


### Place database pods into the "Guaranteed" Quality-of-Service class 

* **Description**: Kubernetes runtime does not kill pods in this class on condition they stay within their resource limits, which is desirable for the DB pods serving production workloads.  To be assigned to that class, pod's resources must equal its limits. The task is to add the `enableGuaranteedQoSClass` or the like option to the Postgres manifest and the operator configmap that forcibly re-write pod resources to match the limits.
* **Recommended skills**: golang, basic Kubernetes abstractions
* **Difficulty**: moderate
* **Mentor(s)**:  Felix Kunde [@FxKu](https://github.com/fxku), Sergey Dudoladov [@sdudoladov](https://github.com/sdudoladov)

### Implement the kubectl plugin for the Postgres CustomResourceDefinition

* **Description**: [kubectl plugins](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) enable extending the Kubernetes command-line client `kubectl`  with commands to manage custom resources. The task is to design and implement a plugin for the `kubectl postgres` command, 
that can enable, for example, correct deletion or major version upgrade of Postgres clusters.
* **Recommended skills**: golang, shell scripting, operational experience with Kubernetes
* **Difficulty**: moderate to medium, depending on the plugin design
* **Mentor(s)**:  Felix Kunde [@FxKu](https://github.com/fxku), Sergey Dudoladov [@sdudoladov](https://github.com/sdudoladov)

### Implement the openAPIV3Schema for the Postgres CRD

* **Description**: at present the operator validates a database manifest on its own. 
It will be helpful to reject erroneous manifests before they reach the operator using the [native Kubernetes CRD validation](https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/#validation). It is up to the student to decide whether to write the schema manually or to adopt existing [schema generator developed for the Prometheus project](https://github.com/ant31/crd-validation).
* **Recommended skills**: golang, JSON schema
* **Difficulty**: medium
* **Mentor(s)**: Sergey Dudoladov [@sdudoladov](https://github.com/sdudoladov) 
* **Issue**: [#388](https://github.com/zalando/postgres-operator/issues/388)

###  Design a solution for the local testing of the operator

* **Description**: The current way of testing is to run minikube, either manually or with some tooling around it like `/run-operator_locally.sh` or Vagrant. This has at least three problems:
First, minikube is a single node cluster, so it is unsuitable for testing vital functions such as pod migration between nodes. Second, minikube starts slowly; that prolongs local testing. 
Third, every contributor  needs to come up with their own solution for local testing. The task is to come up with a better option which will enable us to conveniently and uniformly  run e2e tests locally / potentially in Travis CI.
A promising option is the Kubernetes own [kind](https://github.com/kubernetes-sigs/kind)  
* **Recommended skills**: Docker, shell scripting, basic Kubernetes abstractions
* **Difficulty**: medium to hard depending on the selected desing
* **Mentor(s)**: Dmitry Dolgov [@erthalion](https://github.com/erthalion), Sergey Dudoladov [@sdudoladov](https://github.com/sdudoladov) 
* **Issue**: [#475](https://github.com/zalando/postgres-operator/issues/475)

### Detach a Postgres cluster from the operator for maintenance

* **Description**: sometimes a Postgres cluster requires manual maintenance. During such maintenance the operator should ignore all the changes manually applied to the cluster. 
  Currently the only way to achieve this behavior is to shutdown the operator altogether, for instance by scaling down the operator's own deployment to zero pods. That approach evidently affects all Postgres databases under the operator control and thus is highly undesirable in production Kubernetes clusters. It would be much better to be able to detach only the desired Postgres cluster from the operator for the time being and re-attach it again after maintenance. 
* **Recommended skills**: golang, architecture of a Kubernetes operator
* **Difficulty**: hard - requires significant modification of the operator's internals and careful consideration of the corner cases.
* **Mentor(s)**: Dmitry Dolgov [@erthalion](https://github.com/erthalion), Sergey Dudoladov [@sdudoladov](https://github.com/sdudoladov) 
* **Issue**: [#421](https://github.com/zalando/postgres-operator/issues/421)

### Propose your own idea

Feel free to come up with your own ideas.  For inspiration, 
see [our bug tracker](https://github.com/zalando/postgres-operator/issues), 
the [official `CustomResouceDefinition` docs](https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/) 
and [other operators](https://github.com/operator-framework/awesome-operators).