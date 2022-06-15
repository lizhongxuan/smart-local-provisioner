package main

import (
	"encoding/json"
	"github.com/Sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

var (
	DiskInfoCheckInterval = 20 * time.Second
	DiskConfigmap = "smart-local-disks"
)


//磁盘空间使用率、磁盘inode使用率（df -h和df -i命令）
//磁盘读写次数IOPS (iostat中的r/s、w/s)
//磁盘读写带宽 (iostat中的rkB/s、wkB/s)
//磁盘IO利用率%util (iostat中的%util)
//磁盘队列数 (iostat中的avgqu-sz)
//磁盘读写的延迟时间 (iostat中的r_await、w_await)
type DiskInfo struct {

}

func (p *LocalPathProvisioner) watchAndRefreshDiskConfig() {
	go func() {
		ticker := time.NewTicker(DiskInfoCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := p.refreshDiskConfig(); err != nil {
					logrus.Errorf("failed to load the disk configmap: %v", err)
				}
			case <-p.stopCh:
				logrus.Infof("stop watching disk configmap")
				return
			}
		}
	}()
}

func (p *LocalPathProvisioner) refreshDiskConfig()error {
	if ok := p.diskConfigMutex.TryLock();!ok{
		return nil
	}
	defer p.diskConfigMutex.TryUnLock()

	// get disk configmap
	cm ,err := p.kubeClient.CoreV1().ConfigMaps(p.namespace).Get(DiskConfigmap,v1.GetOptions{})
	if errors.IsNotFound(err) {
		cm ,err = p.kubeClient.CoreV1().ConfigMaps(p.namespace).Create(generatedDiskConfigMap(p.namespace,DiskConfigmap))
		if err != nil{
			return err
		}
	}else if err != nil{
		return err
	}

	cmData := make(map[string]*DiskInfo)
	json.Unmarshal()

	
	


	return nil
}

func generatedDiskConfigMap(ns,name string)*corev1.ConfigMap  {
	return &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: map[string]string{
			"create":time.Now().String(),
		},
	}
}