#!/usr/bin/env bash
#
# Deploy a Postgres Operator to a minikube aka local Kubernetes cluster
# Optionally re-build the operator binary beforehand to test local changes

# Known limitations:
# 1) minikube provides a single node K8s cluster. That is, you will not be able test functions like pod
#    migration between multiple nodes locally
# 2) this script configures the operator via configmap, not the operator CRD


# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'


readonly PATH_TO_LOCAL_OPERATOR_MANIFEST="/tmp/local-postgres-operator-manifest.yaml"
readonly PATH_TO_PORT_FORWARED_KUBECTL_PID="/tmp/kubectl-port-forward.pid"
readonly PATH_TO_THE_PG_CLUSTER_MANIFEST="/tmp/minimal-postgres-manifest.yaml"
readonly LOCAL_PORT="8080"
readonly OPERATOR_PORT="8080"


# minikube needs time to create resources,
# so the script retries actions until all the resources become available
function retry(){

    local -r retry_cmd="$1"
    local -r retry_msg="$2"

    # Time out after three minutes.
    for i in {1..60}; do
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
    echo "Usage: $0 [ -r | --rebuild-operator ] [ -h | --help ] [ -n | --deploy-new-operator-image ] [ -t | --deploy-pg-to-namespace-test ]"
}

function clean_up(){

    echo "==== CLEAN UP PREVIOUS RUN ==== "

    local status
    status=$(minikube status --format "{{.Host}}" || true)

    if [[ "$status" = "Running" ]] || [[ "$status" = "Stopped" ]]; then
        echo "Delete the existing local cluster so that we can cleanly apply resources from scratch..."
        minikube delete
    fi

    if [[ -e "$PATH_TO_LOCAL_OPERATOR_MANIFEST" ]]; then
        rm -v "$PATH_TO_LOCAL_OPERATOR_MANIFEST"
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
        rm -v  "$PATH_TO_PORT_FORWARED_KUBECTL_PID"

    fi
}


function start_minikube(){

    echo "==== START MINIKUBE ===="
    echo "May take a few minutes ..."

    minikube start
    kubectl config set-context minikube

    echo "==== MINIKUBE STATUS ===="
    minikube status
    echo ""
}


function build_operator_binary(){

    # redirecting stderr greatly reduces non-informative output during normal builds
    echo "Build operator binary (stderr redirected to /dev/null)..."
    make clean deps local test > /dev/null 2>&1

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
    sed -e "s/\(image\:.*\:\).*$/\1$TAG/; s/smoke-tested-//" manifests/postgres-operator.yaml > "$PATH_TO_LOCAL_OPERATOR_MANIFEST"

    retry "kubectl apply -f \"$PATH_TO_LOCAL_OPERATOR_MANIFEST\"" "attempt to create $PATH_TO_LOCAL_OPERATOR_MANIFEST resource"
}


function start_operator(){

    echo "==== START OPERATOR ===="
    echo "Certain operations may be retried multiple times..."

    # the order of resource initialization is significant
    local file
    for file  in "configmap.yaml" "operator-service-account-rbac.yaml"
    do
        retry "kubectl  create -f manifests/\"$file\"" "attempt to create $file resource"
    done

    cp  manifests/postgres-operator.yaml $PATH_TO_LOCAL_OPERATOR_MANIFEST

    if [[ "$should_build_custom_operator" = true ]]; then # set in main()
        deploy_self_built_image
    else
        retry "kubectl create -f ${PATH_TO_LOCAL_OPERATOR_MANIFEST}" "attempt to create ${PATH_TO_LOCAL_OPERATOR_MANIFEST} resource"
    fi

    local -r msg="Wait for the postgresql custom resource definition to register..."
    local -r cmd="kubectl get crd | grep --quiet 'postgresqls.acid.zalan.do'"
    retry "$cmd" "$msg "

}


function forward_ports(){

    echo "==== FORWARD OPERATOR PORT $OPERATOR_PORT TO LOCAL PORT $LOCAL_PORT  ===="

    local operator_pod
    operator_pod=$(kubectl get pod -l name=postgres-operator -o jsonpath={.items..metadata.name})

    # Spawn `kubectl port-forward` in the background to keep current terminal
    # responsive. Hide stdout because otherwise there is a note about each TCP
    # connection. Do not hide stderr so port-forward setup errors can be
    # debugged. Sometimes the port-forward setup fails because expected k8s
    # state isn't achieved yet. Try to detect that case and then run the
    # command again (in a finite loop).
    for _attempt in {1..20}; do
        # Delay between retry attempts. First attempt should already be
        # delayed.
        echo "soon: invoke kubectl port-forward command (attempt $_attempt)"
        sleep 5

        # With the --pod-running-timeout=4s argument the process is expected
        # to terminate within about that time if the pod isn't ready yet.
        kubectl port-forward --pod-running-timeout=4s "$operator_pod" "$LOCAL_PORT":"$OPERATOR_PORT" 1> /dev/null &
        _kubectl_pid=$!
        _pf_success=true

        # A successful `kubectl port-forward` setup can pragmatically be
        # detected with a time-based criterion: it is a long-running process if
        # successfully set up. If it does not terminate within deadline then
        # consider the setup successful. Overall, observe the process for
        # roughly 7 seconds. If it terminates before that it's certainly an
        # error. If it did not terminate within that time frame then consider
        # setup successful.
        for ib in {1..7}; do
            sleep 1
            # Portable and non-blocking test: is process still running?
            if kill -s 0 -- "${_kubectl_pid}" >/dev/null 2>&1; then
                echo "port-forward process is still running"
            else
                # port-forward process seems to have terminated, reap zombie
                set +e
                # `wait` is now expected to be non-blocking, and exits with the
                # exit code of pid (first arg).
                wait $_kubectl_pid
                _kubectl_rc=$?
                set -e
                echo "port-forward process terminated with exit code ${_kubectl_rc}"
                _pf_success=false
                break
            fi
        done

        if [ ${_pf_success} = true ]; then
            echo "port-forward setup seems successful. leave retry loop."
            break
        fi

    done

    if [ "${_pf_success}" = false ]; then
        echo "port-forward setup failed after retrying. exit."
        exit 1
    fi

    echo "${_kubectl_pid}" > "$PATH_TO_PORT_FORWARED_KUBECTL_PID"
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


function submit_postgresql_manifest(){

    echo "==== SUBMIT MINIMAL POSTGRES MANIFEST ==== "

    local namespace="default"
    cp manifests/minimal-postgres-manifest.yaml $PATH_TO_THE_PG_CLUSTER_MANIFEST

    if $should_deploy_pg_to_namespace_test; then
          kubectl create namespace test
          namespace="test"
          sed --in-place 's/namespace: default/namespace: test/'  $PATH_TO_THE_PG_CLUSTER_MANIFEST
    fi

    kubectl create -f $PATH_TO_THE_PG_CLUSTER_MANIFEST
    echo "The operator will create the PG cluster with minimal manifest $PATH_TO_THE_PG_CLUSTER_MANIFEST in the ${namespace} namespace"

}


function main(){

    if ! [[ $(basename "$PWD") == "postgres-operator" ]]; then
        echo "Please execute the script only from the root directory of the Postgres Operator repo."
        exit 1
    fi

    trap "echo 'If you observe issues with minikube VM not starting/not proceeding, consider deleting the .minikube dir and/or rebooting before re-running the script'" EXIT

    local should_build_custom_operator=false
    local should_deploy_pg_to_namespace_test=false
    local should_replace_operator_image=false

    while true
    do
        # if the 1st param is unset, use the empty string as a default value
        case "${1:-}" in
            -h | --help)
                display_help
                exit 0
                ;;
            -r | --rebuild-operator) # with minikube restart
                should_build_custom_operator=true
                break
                ;;
            -n | --deploy-new-operator-image) # without minikube restart that takes minutes
                should_replace_operator_image=true
                break
                ;;
            -t | --deploy-pg-to-namespace-test) # to test multi-namespace support locally
                should_deploy_pg_to_namespace_test=true
                break
                ;;
            *)  break
                ;;
        esac
    done

    if ${should_replace_operator_image}; then
       deploy_self_built_image
       exit 0
    fi

    clean_up
    start_minikube
    start_operator
    submit_postgresql_manifest
    forward_ports
    check_health

    exit 0
}


main "$@"
