# Smart Local Provisioner

## Overview

Smart-Local-Provisioner Based on Local-Path-Provisioner. Storage Provisioner provides a way for the Kubernetes users to utilize the local storage in each node. Find available disks based on user configuration or automatically, the Local Path Provisioner will create `hostPath` based persistent volume on the node automatically. It utilizes the features introduced by Kubernetes [Local Persistent Volume feature](https://kubernetes.io/blog/2018/04/13/local-persistent-volumes-beta/), but make it a simpler solution than the built-in `local` volume feature in Kubernetes.


### Features
1. No support for the volume capacity limit currently.
2. If no path is specified, available disks are automatically found.
3. Support  Multiple disks.
4. The remaining disk space is displayed.
5. You can see which pods are running on your disk.
6. Limit the number of pods on your disk.
