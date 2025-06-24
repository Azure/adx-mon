FROM mcr.microsoft.com/azurelinux/base/core:3.0 AS builder

# docker build --file tools/crdgen/Dockerfile --build-arg crd=TestTest -t my-crdgen .
ARG crd
ARG cmd=create

# Install go, make, and ca-certificates
RUN tdnf install -y golang ca-certificates make

# Install controller-gen
RUN go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest && \
    mv /root/go/bin/controller-gen /usr/local/bin/controller-gen

# install operator-sdk
RUN curl -LO https://github.com/operator-framework/operator-sdk/releases/download/v1.23.0/operator-sdk_linux_amd64 && \
    chmod +x operator-sdk_linux_amd64 && \
    mv operator-sdk_linux_amd64 /usr/local/bin/operator-sdk

ADD . /code
WORKDIR /code

# controller-gen and operator-sdk expect a specific directory structure, so we need
# to move some things around.
#
# 1. Create the directory structure config/crd/bases and config/default
RUN mkdir -p config/crd/bases config/default
# 2. Move the CRD yaml files to config/crd/bases
RUN mv kustomize/bases/* config/crd/bases
# 3. Add a adx-mon.azure.com_ prefix to all our CRD yaml files
RUN for file in config/crd/bases/*; do mv $file config/crd/bases/adx-mon.azure.com_$(basename $file _crd.yaml).yaml; done
# 4. Create an empty hack file
RUN mkdir hack && touch hack/boilerplate.go.txt
# 5. Overwrite our existing Makefile to add a generate target
RUN echo -e ".PHONY: generate\ngenerate:\n\tcontroller-gen object:headerFile=\"hack/boilerplate.go.txt\" paths=\"./...\"" > Makefile

# Now we need to create config/default/kustomization.yaml with the following content:
RUN echo -e "namespace: adx-mon\nnamePrefix: \"\"\nresources:\n- ../crd" > config/default/kustomization.yaml
# And config/crd/kustomization.yaml with the following content:
RUN echo -e "resources:\n" > config/crd/kustomization.yaml
# for each of our existing crd files, write them into our resources:
RUN for file in config/crd/bases/*; do echo "- $(basename $file)" >> config/crd/kustomization.yaml; done
RUN echo "#+kubebuilder:scaffold:crdkustomizeresource" >> config/crd/kustomization.yaml
RUN echo -e "patchesStrategicMerge:\nconfigurations:\n- kustomizeconfig.yaml" >> config/crd/kustomization.yaml

# Copy our project file
RUN cp /code/tools/crdgen/PROJECT /code/PROJECT
# And our main.go file
RUN cp /code/tools/crdgen/main.go /code/main.go

# Now generate our new CRD or update our existing CRDs
RUN if [ "$cmd" = "create" ]; then \
        operator-sdk create api --group adx-mon --version v1 --kind "${crd}" --resource=true --controller=false; \
    elif [ "$cmd" != "update" ]; then \
        echo "Invalid cmd value: ${cmd}" && exit 1; \
    fi && \
    controller-gen rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases && \
    controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..." && \
    echo "Running CEL validation patching..." && \
    go run /code/tools/cmd/patch-cel-validations/main.go -api-dir=/code/api/v1 -crd-dir=/code/config/crd/bases

# Copy generated files into the "bin" directory
RUN mkdir -p /code/bin && \
    if [ "$cmd" = "create" ]; then \
        MY_CRD=$(echo ${crd} | tr A-Z a-z) && \
        cp /code/api/v1/${MY_CRD}_types.go /code/bin/${MY_CRD}_types.go; \
    fi && \
    cp /code/api/v1/zz_generated.deepcopy.go /code/bin/zz_generated.deepcopy.go && \
    cp /code/PROJECT /code/bin/PROJECT && \
    for file in $(ls /code/config/crd/bases); do \
        base=$(basename $file); \
        rawname=$(echo $base | sed 's/^adx-mon\.azure\.com_//'); \
        if echo $rawname | grep -q '_crd\.yaml$$'; then \
            finalname=$rawname; \
        else \
            finalname=$(echo $rawname | sed -E 's/(\.yaml)$$/_crd\1/'); \
        fi; \
        cp /code/config/crd/bases/$file /code/bin/$finalname; \
    done
