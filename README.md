# Cloud Provider secret controller

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## What is cloudprovider-secret-controller

With `cloudprovider-secret-controller`, you can take care of updating Hive &amp; Ansible related secrets that are created from your Cloud Provider Secrets.

Go to the [Contributing guide](CONTRIBUTING.md) to learn how to get involved.

## Getting started

- ### Steps for development: 

  - Compile the code by running `make compile`
  - Run the controller manually `./build/_output/cloudprovider-secret-controller`
  - Run the go file manually `go run ./cmd/manager/main.go`


  - Push an image to your provite repository:
    ```bash
    export VERSION=0.1
    export REPO_URL=quay.io/MY_ORGANIZATION_OR_USERNAME

    make push
    ```


- ### Steps for deployment:

  - Connect to the OpenShift cluster acting as the hub for Open Cluster Management
    ```bash
    oc apply -k deploy/controller
    ```
  - Even though this controller deploys as a single pod, it uses leader election to make sure only one instance is ever running.


- ### Steps for test:

  - `make unit-tests`

- Check the [Security guide](SECURITY.md) if you need to report a security issue.


## References

- The `cloudprovider-secret-controller` is part of the `open-cluster-management` community. For more information, visit: [open-cluster-management.io](https://open-cluster-management.io).
- Optional: List and link of additional references if needed.
