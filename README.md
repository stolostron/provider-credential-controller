# Provider-credential-controller

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## What is provider-credential-controller

With `provider-credential-controller`, your cluster secrets will be automatically updated when making changes to the Provider Crednetial secrets.

Go to the [Contributing guide](CONTRIBUTING.md) to learn how to get involved.

## Getting started

- ### Steps for development: 

  - Compile the code by running
    ```bash
    make compile

    ./build/_output/manager  # Execute the binary
    ```
  
  - Run the _go_ file manually
    ```bash
    go run ./cmd/manager/main.go
    ```
  
  - Push an image to your repository
    ```bash
    export VERSION=0.1 # Specify a version, must be edited in ./deploy/controller/deployment.yaml
    export REPO_URL=quay.io/MY_ORGANIZATION_OR_USERNAME

    make push
    ```


- ### Steps for deployment:

  - Connect to the OpenShift cluster acting as the hub for Open Cluster Management
    ```bash
    oc apply -k deploy/controller
    ```
    - Even though this controller deploys as a single pod, it uses leader election to make sure only one instance is ever running.
    - Even if the controller is interupted while updating secrets, when it restarts, it will continue the process until all copied secrets are updated with the new values from the Provider Credential secret.

- ### Steps for testing:

  - Running unit tests:
    ```bash
    make unit-tests
    ```

  - Running scale testing (3000 copied secrets)
    - Connect to an OpenShift cluster
    - Make sure either the controller is deployed, [see Steps for deployment](#Steps-for-deployment) or launched from the command line, [see Steps for development](#Steps-for-development)
      ```bash
      # Create namespace
      oc new-project providers

      make scale-up   # This creates a fake Ansible Provider Secret, and makes 3000 copies
                      # To changes the number of copies edit ./controller/provider-credential-controller_scale_test.go
                      #     const SecretCount = 3000

      make scale-test # This makes FOUR token changes to the Provider secret without waiting

      make scale-down # Removes a fake Ansible Provider Secret and deletes 3000 copies
      ```
    - This test executes a sequence of four token updates, not waiting for the 3000 copies to be reconciled. This validates that we do not lose track of the Provider secret updates, even when there is a processing delay in reconciling each copied secret.


- Check the [Security guide](SECURITY.md) if you need to report a security issue.


