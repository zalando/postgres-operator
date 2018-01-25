#!/bin/bash

# unofficial bash strict mode w/o the -e option
# -e breaks "eval $cmd" in the retry function
set -uo pipefail
IFS=$'\n\t'

function retry(){ # timeouts after 1 minutes
    cmd="$1"
    retryMsg="$2"
    for i in {1..20}; do
	if  eval "$cmd"; then
            return 0
        fi
	echo "$retryMsg"
        sleep 3
    done

    return 1
}

function build_operator_binary(){ 
    make tools > /dev/null 2>&1 &&
	make deps  > /dev/null 2>&1 &&
	make local > /dev/null 2>&1
}

# the fastest way to run your docker image locally is to reuse the docker from minikube. 
function deploy_self_built_image() {

    echo "==== DEPLOY CUSTOM OPERATOR IMAGE ==== "

    echo "Build operator binary (stderr redirected to /dev/null)..."
    build_operator_binary
    
    # set docker env vars so that docker can talk to the Docker daemon inside the minikube VM
    eval $(minikube docker-env)

    # image tag consists of a git tag or a unique commit prefix
    # and the "-dev" suffix if there are uncommited changes in the working dir
    export TAG=$(git describe --tags --always --dirty="-dev")

    # build the image
    echo "Build the operator Docker image (stderr redirected to /dev/null)..."
    make docker > /dev/null 2>&1
    
    # update the tag in the postgres operator conf
    # since the image with this tag is already present on the machine,
    # docker should not attempt to fetch it from the registry due to imagePullPolicy
    file="/tmp/local-postgres-operator.yaml"
    sed -e "s/\(image\:.*\:\).*$/\1$TAG/" manifests/postgres-operator.yaml > "$file"
    
    retry "kubectl create -f \"$file\"" "attempt to create $file resource"
}

function display_help(){
    echo "Usage: ./run_locally.sh [ -r | --rebuild-operator ] [ -h | --help ]"    
}

# parse options
should_build_operator=false
while true
do
    case "${1:-''}" in # if 1st is usnet, use the mety string as a default value

	-h | --help)
	    display_help
	    exit 0
	    ;;
	-r | --rebuild-operator)
	    should_build_operator=true
	    shift 2
	    break
	    ;;
	--) shift
	    break;;
	*)	     break
		     ;;
    esac
done


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
for file  in "configmap.yaml" "serviceaccount.yaml" 
do
    retry "kubectl  create -f manifests/\"$file\"" "attempt to create $file resource"
done


if [ "$should_build_operator" = true ]; then
    deploy_self_built_image
else
    retry "kubectl  create -f manifests/postgres-operator.yaml" "attempt to create /postgres-operator.yaml resource" 
fi

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
    echo "To stop it cleanly, run 'minikube delete'"
else
    echo "==== FAILURE: OPERATOR DID NOT START OR PORT FORWARDING DID NOT WORK"
    echo "This *might* have left the minikube VM image in inconsistent state."
    echo "If you observe strange issues, try deleting .minikube dir and re-running the script"
    exit 1
fi

