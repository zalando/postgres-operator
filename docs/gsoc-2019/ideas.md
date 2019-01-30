
# Google Summer of Code 2019

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
* **Issue**: [#388](https://github.com/zalando-incubator/postgres-operator/issues/388)

### Detach a Postgres cluster from the operator for maintenance

* **Description**: sometimes a Postgres cluster requires manual maintenance. During such maintenance the operator should ignore all the changes manually applied to the cluster. 
  Currently the only way to achieve this behavior is to shutdown the operator altogether, for instance by scaling down the operator's own deployment to zero pods. That approach evidently affects all Postgres databases under the operator control and thus is highly undesirable in production Kubernetes clusters. It would be much better to be able to detach only the desired Postgres cluster from the operator for the time being and re-attach it again after maintenance. 
* **Recommended skills**: golang, architecture of a Kubernetes operator
* **Difficulty**: hard - requires significant modification of the operator's internals and careful consideration of the corner cases.
* **Mentor(s)**: Dmitry Dolgov [@erthalion](https://github.com/erthalion), Sergey Dudoladov [@sdudoladov](https://github.com/sdudoladov) 
* **Issue**: [#421](https://github.com/zalando-incubator/postgres-operator/issues/421)

### Propose your own idea

Feel free to come up with your own ideas.  For inspiration, 
see [our bug tracker](https://github.com/zalando-incubator/postgres-operator/issues), 
the [official `CustomResouceDefinition` docs](https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/) 
and [other operators](https://github.com/operator-framework/awesome-operators).