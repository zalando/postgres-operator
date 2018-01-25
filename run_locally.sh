#!/bin/bash

# unofficial bash strict mode w/o the -e option
# -e breaks "eval $cmd" in the retry function
set -uo pipefail
IFS=$'\n\t'

function retry(){ # timeouts after 1 minutes
    cmd="$1"
    retryMsg="$2"
    for i in {1..20}; do
	eval "$cmd"
	if [ $? -eq 0 ]; then
            return 0
        fi
	echo "$retryMsg"
        sleep 3
    done

    return 1
}

echo "==== CLEAN UP PREVIOUS RUN ==== "

status=$(minikube status --format "{{.MinikubeStatus}}")
if [ "$status" = "Running" ] || [ "$status" = "Stopped" ]; then
    echo "Delete the existing local cluster so that we can cleanly apply resources from scratch..."
    minikube delete
fi

# the kubectl process does the port-forwarding between operator and local ports
# we restart the process to bind to the same port again (see end of script)
if [ -e /tmp/kubectl-port-forward.pid ]; then
    
    pid=$(cat /tmp/kubectl-port-forward.pid)
    # the process will die if a minikube is stopped manually between two invocations of the script
    if ps --pid "$pid" > /dev/null; then
	echo "Kill the kubectl process responsible for port forwarding for minikube so that we can re-use the same ports for forwarding later..."
	kill "$pid"
    fi
    rm /tmp/kubectl-port-forward.pid
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

msg="Wait for the postgresql custom resource definition to register..."
cmd="kubectl get crd | grep --quiet 'postgresqls.acid.zalan.do'"
retry "$cmd" "$msg "

kubectl create -f manifests/complete-postgres-manifest.yaml

localPort="8080"
operatorPort="8080"
echo "==== FORWARD OPERATOR PORT $operatorPort TO LOCAL PORT $localPort  ===="
operatorPod=$(kubectl get pod -l name=postgres-operator -o jsonpath={.items..metadata.name})
# runs in the background to keep current terminal responsive
# stdout redirect removes the info message about forwarded ports; the message sometimes garbles the cli prompt
kubectl port-forward "$operatorPod" "$localPort":"$operatorPort" &> /dev/null &
pgrep --newest "kubectl" > /tmp/kubectl-port-forward.pid

echo "==== RUN HEALTH CHECK ==== "
checkCmd="curl --location --silent http://127.0.0.1:$localPort/clusters &> /dev/null"
echo "Command for checking: $checkCmd"
checkMsg="Wait for port forwarding to take effect"

if  retry "$checkCmd" "$checkMsg" ; then
    echo "==== SUCCESS: OPERATOR IS RUNNING ==== "
else
    echo "==== FAILURE: OPERATOR DID NOT START OR PORT FORWARDING DID NOT WORK"
    exit 1
fi
