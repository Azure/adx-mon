# About

Generate new CRDs using _controller-gen_ and _operator-sdk_ using a container so that we don't have to adopt the project layout that these tools require. 

# Update an existing CRD

If you want to mutate an existing CRD, you can run the command

`make generate-crd CMD=update` from the root directory

# Create a new CRD

Run the command `make generate-crd CRD=MY_CRD CMD=create` from the root directory