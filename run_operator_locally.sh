#!/usr/bin/env bash
#
# Deploy a Postgres operator to a minikube aka local Kubernetes cluster
# Optionally re-build the operator binary beforehand to test local changes


# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'


readonly PATH_TO_LOCAL_OPERATOR_MANIFEST="/tmp/local-postgres-operator-manifest.yaml"
readonly PATH_TO_PORT_FORWARED_KUBECTL_PID="/tmp/kubectl-port-forward.pid"
readonly LOCAL_PORT="8080"
readonly OPERATOR_PORT="8080"


# minikube needs time to create resources,
# so the script retries actions until all the resources become available
function retry(){

    local -r retry_cmd="$1"
    local -r retry_msg="$2"

    # times out after 1 minute
    for i in {1..20}; do
        if  eval "$retry_cmd"; then
            return 0
        fi
        echo "$retry_msg"
        sleep 3
    done

    >2& echo "The command $retry_cmd timed out"
    return 1
}


function display_help(){
    echo "Usage: $0 [ -r | --rebuild-operator ] [ -h | --help ]"
}


function clean_up(){

    echo "==== CLEAN UP PREVIOUS RUN ==== "

    local status
    status=$(minikube status --format "{{.MinikubeStatus}}" || true)

    if [[ "$status" = "Running" ]] || [[ "$status" = "Stopped" ]]; then
        echo "Delete the existing local cluster so that we can cleanly apply resources from scratch..."
        minikube delete
    fi

    if [[ -e "$PATH_TO_LOCAL_OPERATOR_MANIFEST" ]]; then
        rm --verbose "$PATH_TO_LOCAL_OPERATOR_MANIFEST"
    fi

    # the kubectl process does the port-forwarding between operator and local ports
    # we restart the process to bind to the same port again (see end of script)
    if [[ -e "$PATH_TO_PORT_FORWARED_KUBECTL_PID" ]]; then

        local pid
        pid=$( < "$PATH_TO_PORT_FORWARED_KUBECTL_PID")

        # the process dies if a minikube stops between two invocations of the script
        if kill "$pid" > /dev/null  2>&1; then
            echo "Kill the kubectl process responsible for port forwarding for minikube so that we can re-use the same ports for forwarding later..."
        fi
        rm --verbose  "$PATH_TO_PORT_FORWARED_KUBECTL_PID"

    fi
}


function start_minikube(){

    echo "==== START MINIKUBE ==== "
    echo "May take a few minutes ..."

    minikube start
    kubectl config set-context minikube

    echo "==== MINIKUBE STATUS ==== "
    minikube status

}


function build_operator_binary(){

    # redirecting stderr greatly reduces non-informative output during normal builds
    echo "Build operator binary (stderr redirected to /dev/null)..."
    make tools deps local > /dev/null 2>&1

}


function deploy_self_built_image() {

    echo "==== DEPLOY CUSTOM OPERATOR IMAGE ==== "

    build_operator_binary

    # the fastest way to run a docker image locally is to reuse the docker from minikube
    # set docker env vars so that docker can talk to the Docker daemon inside the minikube
    eval $(minikube docker-env)

    # image tag consists of a git tag or a unique commit prefix
    # and the "-dev" suffix if there are uncommited changes in the working dir
    local -x TAG
    TAG=$(git describe --tags --always --dirty="-dev")
    readonly TAG

    # build the image
    make docker > /dev/null 2>&1

    # update the tag in the postgres operator conf
    # since the image with this tag already exists on the machine,
    # docker should not attempt to fetch it from the registry due to imagePullPolicy
    sed --expression "s/\(image\:.*\:\).*$/\1$TAG/" manifests/postgres-operator.yaml > "$PATH_TO_LOCAL_OPERATOR_MANIFEST"

    retry "kubectl create -f \"$PATH_TO_LOCAL_OPERATOR_MANIFEST\"" "attempt to create $PATH_TO_LOCAL_OPERATOR_MANIFEST resource"
}


function start_operator(){

    echo "==== START OPERATOR ==== "
    echo "Certain operations may be retried multiple times..."

    # the order of resource initialization is significant
    local file
    for file  in "configmap.yaml" "serviceaccount.yaml"
    do
        retry "kubectl  create -f manifests/\"$file\"" "attempt to create $file resource"
    done

    if [[ "$should_build_custom_operator" = true ]]; then # set in main()
        deploy_self_built_image
    else
        retry "kubectl  create -f manifests/postgres-operator.yaml" "attempt to create /postgres-operator.yaml resource"
    fi

    local -r msg="Wait for the postgresql custom resource definition to register..."
    local -r cmd="kubectl get crd | grep --quiet 'postgresqls.acid.zalan.do'"
    retry "$cmd" "$msg "

    kubectl create -f manifests/complete-postgres-manifest.yaml
}


function forward_ports(){

    echo "==== FORWARD OPERATOR PORT $OPERATOR_PORT TO LOCAL PORT $LOCAL_PORT  ===="

    local operator_pod
    operator_pod=$(kubectl get pod -l name=postgres-operator -o jsonpath={.items..metadata.name})

    # runs in the background to keep current terminal responsive
    # stdout redirect removes the info message about forwarded ports; the message sometimes garbles the cli prompt
    kubectl port-forward "$operator_pod" "$LOCAL_PORT":"$OPERATOR_PORT" &> /dev/null &

    echo $! > "$PATH_TO_PORT_FORWARED_KUBECTL_PID"
}


function check_health(){

    echo "==== RUN HEALTH CHECK ==== "

    local -r check_cmd="curl --location --silent --output /dev/null http://127.0.0.1:$LOCAL_PORT/clusters"
    local -r check_msg="Wait for port forwarding to take effect"
    echo "Command for checking: $check_cmd"

    if  retry "$check_cmd" "$check_msg"; then
        echo "==== SUCCESS: OPERATOR IS RUNNING ==== "
        echo "To stop it cleanly, run 'minikube delete'"
    else
        >2& echo "==== FAILURE: OPERATOR DID NOT START OR PORT FORWARDING DID NOT WORK"
        exit 1
    fi
}


function main(){

    if ! [[ $(basename "$PWD") == "postgres-operator" ]]; then
        echo "Please execute the script only from the root directory of the Postgres opepator repo."
        exit 1
    fi

    trap "echo 'If you observe issues with minikube VM not starting/not proceeding, consider deleting the .minikube dir and/or rebooting before re-running the script'" EXIT

    local should_build_custom_operator=false # used in start_operator()
    while true
    do
        # if the 1st param is unset, use the empty string as a default value
        case "${1:-}" in
            -h | --help)
                display_help
                exit 0
                ;;
            -r | --rebuild-operator)
                should_build_custom_operator=true
                break
                ;;
            *)  break
                ;;
        esac
    done

    clean_up
    start_minikube
    start_operator
    forward_ports
    check_health

    exit 0
}


main "$@"
