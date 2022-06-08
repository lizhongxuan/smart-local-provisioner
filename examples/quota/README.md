# Overview
this is an example to enable quota for xfs 

# Usage
> 1. build a helper image using the sample dockerfile to replace helper image xxx/storage-xfs-quota:v0.1 at configmap(helperPod.yaml) of debug.yaml.
> 2. use the sample setup and teardown scripts contained within the kustomization.

Notice:
> 1. make sure the path at nodePathMap is the mountpoint of xfs which enables pquota

# debug
```Bash
> git clone https://github.com/lizhongxuan/storage-provisioner.git
> cd storage-provisioner
> go build
> kubectl apply -k examples/quota
> kubectl delete -n local-path-storage deployment storage-provisioner
> ./storage-provisioner --debug start --namespace=local-storage
```
