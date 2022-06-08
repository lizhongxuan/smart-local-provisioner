#!/bin/bash

set -e

base="${GOPATH}/src/github.com/lizhongxuan/smart-local-provisioner"
files=`find ${base}/deploy/ |grep yaml |sort`

project="lizhongxuan\/smart-local-provisioner"
latest=`cat ${base}/bin/latest_image`
echo latest image ${latest}

escaped_image=${latest//\//\\\/}

for f in $files
do
	sed -i "s/image\:\ ${project}:.*/image\:\ ${escaped_image}/g" $f
	sed -i "s/-\ ${project}:.*/-\ ${escaped_image}/g" $f
done

