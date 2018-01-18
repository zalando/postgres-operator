#!/bin/bash

set -uo pipefail 
IFS=$'\n\t'

# timeouts after 3 minutes
function retry(){
    cmd="$1"
    retryMsg="$2"
    for i in {1..90}; do
	eval "$cmd"
	if [ $? -eq 0 ]; then
         return 0
        fi
	echo "$retryMsg"
        sleep 2
    done
    
    return 1
}


echo "==== CLEAN UP PREVIOUS RUN ==== "

status=$(minikube status --format "{{.MinikubeStatus}}")
if [ "$status" = "Running" ]; then 
    minikube delete
fi

# the kubectl process does the port-forwarding between operator and local ports
# we restart the process to be able to bind to the same port again (see end of script)
echo "Kill kubectl process to re-init port-forwarding for minikube"
kubepid=$(pidof "kubectl")
if [ $? -eq 0 ]; then
    kill "$kubepid"
fi


echo "==== START MINIKUBE ==== "
echo "May take a few minutes ..."
minikube start 
kubectl config set-context minikube

echo "==== MINIKUBE STATUS ==== "
minikube status

echo "==== START OPERATOR ==== "
# the order of files is significant
for file  in "configmap.yaml" "serviceaccount.yaml" "postgres-operator.yaml"
do
   retry "kubectl  create -f manifests/\"$file\"" "attempt to create $file resource" 
done

msg="Wait for the postgresql custom resource definition to register."
cmd="kubectl get crd | grep 'postgresqls.acid.zalan.do' &> /dev/null"
retry "$cmd" "$msg "

kubectl create -f manifests/complete-postgres-manifest.yaml

localPort="8080"
operatorPort="8080"
echo "==== FORWARD OPERATOR PORT $operatorPort TO LOCAL PORT $localPort  ===="
operatorPod=$(kubectl get pod -l name=postgres-operator -o jsonpath={.items..metadata.name})
# runs in the background to keep current terminal responsive
# 1> redirects stdout to remove the info message about forwarded ports; the message sometimes garbles the cli prompt
kubectl port-forward "$operatorPod" "$localPort":"$operatorPort" 1> /dev/null &


echo "==== RUN HEALTH CHECK ==== "
checkCmd="curl -L http://127.0.0.1:$localPort/clusters &> /dev/null"
checkMsg="Wait for port forwarding to take effect"
retry "$checkCmd"  "$checkMsg"

if [ $? -eq 0 ]; then
    echo "==== SUCCESS: OPERATOR IS RUNNING ==== "
else
    echo "Operator did not start or port forwarding did not work"
    exit 1
fi




