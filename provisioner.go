package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"smart-local-provisioner/util/trylock"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	pvController "sigs.k8s.io/sig-storage-lib-external-provisioner/controller"
)

type ActionType string

const (
	ActionTypeCreate = "create"
	ActionTypeDelete = "delete"
)

const (
	KeyNode = "kubernetes.io/hostname"

	NodeDefaultNonListedNodes = "DEFAULT_PATH_FOR_NON_LISTED_NODES"

	helperScriptDir     = "/script"
	helperDataVolName   = "data"
	helperScriptVolName = "script"

	envVolDir  = "VOL_DIR"
	envVolMode = "VOL_MODE"
	envVolSize = "VOL_SIZE_BYTES"
)

const (
	defaultCmdTimeoutSeconds = 120
)

var (
	ConfigFileCheckInterval = 30 * time.Second

	HelperPodNameMaxLength = 128
)

type LocalPathProvisioner struct {
	stopCh             chan struct{}
	kubeClient         *clientset.Clientset
	namespace          string
	helperImage        string
	serviceAccountName string

	config        *Config
	configData    *ConfigData
	configFile    string
	configMapName string
	configMutex   *sync.RWMutex
	diskConfigMutex *trylock.TryLock
	diskConifgmap map[string]*DiskInfo
	helperPod     *v1.Pod
}

type NodePathMapData struct {
	PreserveVolum int64 `json:"preserve_volum,omitempty"` // 磁盘保留空间,当FreeVolum少于PreserveVolum时,则不分配
	Paths []string `json:"paths,omitempty"`
}

type ConfigData struct {
	NodePathMap map[string]*NodePathMapData `json:"nodePathMap,omitempty"`
	CmdTimeoutSeconds int `json:"cmdTimeoutSeconds,omitempty"`
}

type NodePathMap struct {
	PreserveVolum int64 // 磁盘保留空间,FreeVolum少于PreserveVolum则不分配
	Paths map[string]*Disk
}

type Disk struct {
	MaxVolume int64 // 磁盘最大空间,以M为单位
	FreeVolum int64 // 磁盘剩余空间,以M为单位
	Pods map[string][]string // 使用在该路径上的pod.key为命名空间,value为podname数组
}

type Config struct {
	NodePathMap       map[string]*NodePathMap
	CmdTimeoutSeconds int
}

func NewProvisioner(stopCh chan struct{}, kubeClient *clientset.Clientset,
	configFile, namespace, helperImage, configMapName, serviceAccountName, helperPodYaml string) (*LocalPathProvisioner, error) {
	p := &LocalPathProvisioner{
		stopCh: stopCh,

		kubeClient:         kubeClient,
		namespace:          namespace,
		helperImage:        helperImage,
		serviceAccountName: serviceAccountName,

		// config will be updated shortly by p.refreshConfig()
		config:        nil,
		configFile:    configFile,
		configData:    nil,
		configMapName: configMapName,
		configMutex:   &sync.RWMutex{},
		diskConfigMutex: trylock.Build(),
	}
	var err error
	p.helperPod, err = loadHelperPodFile(helperPodYaml)
	if err != nil {
		return nil, err
	}

	if err := p.refreshDiskConfig(); err != nil {
		return nil, err
	}
	if err := p.refreshConfig(); err != nil {
		return nil, err
	}
	p.watchAndRefreshDiskConfig()
	p.watchAndRefreshConfig()
	return p, nil
}

// refreshConfig 刷新配置,启动或每隔30秒调用
func (p *LocalPathProvisioner) refreshConfig() error {
	p.configMutex.Lock()
	defer p.configMutex.Unlock()

	configData, err := loadConfigFile(p.configFile)
	if err != nil {
		return err
	}
	//// no need to update
	//if reflect.DeepEqual(configData, p.configData) {
	//	return nil
	//}

	// collect disk's info on nodes.


	config, err := canonicalizeConfig(configData)
	if err != nil {
		return err
	}
	// only update the config if the new config file is valid
	p.configData = configData
	p.config = config

	output, err := json.Marshal(p.configData)
	if err != nil {
		return err
	}
	logrus.Debugf("Applied config: %v", string(output))

	return err
}

func (p *LocalPathProvisioner) watchAndRefreshConfig() {
	go func() {
		ticker := time.NewTicker(ConfigFileCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := p.refreshConfig(); err != nil {
					logrus.Errorf("failed to load the new config file: %v", err)
				}
			case <-p.stopCh:
				logrus.Infof("stop watching config file")
				return
			}
		}
	}()
}

// TODO: 有设置则使用设置路径,没有则返回node上磁盘空间最多的路径
func (p *LocalPathProvisioner) getRandomPathOnNode(node string) (string, error) {
	p.configMutex.RLock()
	defer p.configMutex.RUnlock()

	if p.config == nil {
		return "", fmt.Errorf("no valid config available")
	}

	c := p.config
	npMap := c.NodePathMap[node]
	if npMap == nil {
		npMap = c.NodePathMap[NodeDefaultNonListedNodes]
		if npMap == nil {
			return "", fmt.Errorf("config doesn't contain node %v, and no %v available", node, NodeDefaultNonListedNodes)
		}
		logrus.Debugf("config doesn't contain node %v, use %v instead", node, NodeDefaultNonListedNodes)
	}
	paths := npMap.Paths
	if len(paths) == 0 {
		return "", fmt.Errorf("no local path available on node %v", node)
	}

	path := ""
	err := fmt.Errorf("no local path available on node %v", node)
	var freevolum int64
	for pa,disk := range paths {
		if disk.FreeVolum < npMap.PreserveVolum{
			if path == "" {
				err = fmt.Errorf("FreeVolum less than PreserveVolum on node %v", node)
			}
			continue
		}
		if disk.FreeVolum > freevolum {
			path = pa
			err = nil
		}
	}
	return path, err
}

func (p *LocalPathProvisioner) Provision(opts pvController.ProvisionOptions) (*v1.PersistentVolume, error) {
	pvc := opts.PVC
	if pvc.Spec.Selector != nil {
		return nil, fmt.Errorf("claim.Spec.Selector is not supported")
	}
	for _, accessMode := range pvc.Spec.AccessModes {
		if accessMode != v1.ReadWriteOnce {
			return nil, fmt.Errorf("Only support ReadWriteOnce access mode")
		}
	}
	node := opts.SelectedNode
	if opts.SelectedNode == nil {
		return nil, fmt.Errorf("configuration error, no node was specified")
	}

	basePath, err := p.getRandomPathOnNode(node.Name)
	if err != nil {
		return nil, err
	}

	name := opts.PVName
	folderName := strings.Join([]string{name, opts.PVC.Namespace, opts.PVC.Name}, "_")

	path := filepath.Join(basePath, folderName)
	logrus.Infof("Creating volume %v at %v:%v", name, node.Name, path)

	storage := pvc.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	provisionCmd := []string{"/bin/sh", "/script/setup"}
	if err := p.createHelperPod(ActionTypeCreate, provisionCmd, volumeOptions{
		Name:        name,
		Path:        path,
		Mode:        *pvc.Spec.VolumeMode,
		SizeInBytes: storage.Value(),
		Node:        node.Name,
	}); err != nil {
		return nil, err
	}

	fs := v1.PersistentVolumeFilesystem
	hostPathType := v1.HostPathDirectoryOrCreate

	valueNode, ok := node.GetLabels()[KeyNode]
	if !ok {
		valueNode = node.Name
	}

	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *opts.StorageClass.ReclaimPolicy,
			AccessModes:                   pvc.Spec.AccessModes,
			VolumeMode:                    &fs,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): pvc.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: path,
					Type: &hostPathType,
				},
			},
			NodeAffinity: &v1.VolumeNodeAffinity{
				Required: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      KeyNode,
									Operator: v1.NodeSelectorOpIn,
									Values: []string{
										valueNode,
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func (p *LocalPathProvisioner) Delete(pv *v1.PersistentVolume) (err error) {
	defer func() {
		err = errors.Wrapf(err, "failed to delete volume %v", pv.Name)
	}()
	path, node, err := p.getPathAndNodeForPV(pv)
	if err != nil {
		return err
	}
	if pv.Spec.PersistentVolumeReclaimPolicy != v1.PersistentVolumeReclaimRetain {
		logrus.Infof("Deleting volume %v at %v:%v", pv.Name, node, path)
		storage := pv.Spec.Capacity[v1.ResourceName(v1.ResourceStorage)]
		cleanupCmd := []string{"/bin/sh", "/script/teardown"}
		if err := p.createHelperPod(ActionTypeDelete, cleanupCmd, volumeOptions{
			Name:        pv.Name,
			Path:        path,
			Mode:        *pv.Spec.VolumeMode,
			SizeInBytes: storage.Value(),
			Node:        node,
		}); err != nil {
			logrus.Infof("clean up volume %v failed: %v", pv.Name, err)
			return err
		}
		return nil
	}
	logrus.Infof("Retained volume %v", pv.Name)
	return nil
}

func (p *LocalPathProvisioner) getPathAndNodeForPV(pv *v1.PersistentVolume) (path, node string, err error) {
	defer func() {
		err = errors.Wrapf(err, "failed to delete volume %v", pv.Name)
	}()

	hostPath := pv.Spec.PersistentVolumeSource.HostPath
	if hostPath == nil {
		return "", "", fmt.Errorf("no HostPath set")
	}
	path = hostPath.Path

	nodeAffinity := pv.Spec.NodeAffinity
	if nodeAffinity == nil {
		return "", "", fmt.Errorf("no NodeAffinity set")
	}
	required := nodeAffinity.Required
	if required == nil {
		return "", "", fmt.Errorf("no NodeAffinity.Required set")
	}

	node = ""
	for _, selectorTerm := range required.NodeSelectorTerms {
		for _, expression := range selectorTerm.MatchExpressions {
			if expression.Key == KeyNode && expression.Operator == v1.NodeSelectorOpIn {
				if len(expression.Values) != 1 {
					return "", "", fmt.Errorf("multiple values for the node affinity")
				}
				node = expression.Values[0]
				break
			}
		}
		if node != "" {
			break
		}
	}
	if node == "" {
		return "", "", fmt.Errorf("cannot find affinited node")
	}
	return path, node, nil
}

type volumeOptions struct {
	Name        string
	Path        string
	Mode        v1.PersistentVolumeMode
	SizeInBytes int64
	Node        string
}

func (p *LocalPathProvisioner) createHelperPod(action ActionType, cmd []string, o volumeOptions) (err error) {
	defer func() {
		err = errors.Wrapf(err, "failed to %v volume %v", action, o.Name)
	}()
	if o.Name == "" || o.Path == "" || o.Node == "" {
		return fmt.Errorf("invalid empty name or path or node")
	}
	if !filepath.IsAbs(o.Path) {
		return fmt.Errorf("volume path %s is not absolute", o.Path)
	}
	o.Path = filepath.Clean(o.Path)
	parentDir, volumeDir := filepath.Split(o.Path)
	hostPathType := v1.HostPathDirectoryOrCreate
	lpvVolumes := []v1.Volume{
		{
			Name: helperDataVolName,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: parentDir,
					Type: &hostPathType,
				},
			},
		},
		{
			Name: helperScriptVolName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.configMapName,
					},
					Items: []v1.KeyToPath{
						{
							Key:  "setup",
							Path: "setup",
						},
						{
							Key:  "teardown",
							Path: "teardown",
						},
					},
				},
			},
		},
	}
	lpvTolerations := []v1.Toleration{
		{
			Operator: v1.TolerationOpExists,
		},
	}
	helperPod := p.helperPod.DeepCopy()

	scriptMount := addVolumeMount(&helperPod.Spec.Containers[0].VolumeMounts, helperScriptVolName, helperScriptDir)
	scriptMount.MountPath = helperScriptDir
	dataMount := addVolumeMount(&helperPod.Spec.Containers[0].VolumeMounts, helperDataVolName, parentDir)
	parentDir = dataMount.MountPath
	parentDir = strings.TrimSuffix(parentDir, string(filepath.Separator))
	volumeDir = strings.TrimSuffix(volumeDir, string(filepath.Separator))
	if parentDir == "" || volumeDir == "" || !filepath.IsAbs(parentDir) {
		// it covers the `/` case
		return fmt.Errorf("invalid path %v for %v: cannot find parent dir or volume dir or parent dir is relative", action, o.Path)
	}
	env := []v1.EnvVar{
		{Name: envVolDir, Value: filepath.Join(parentDir, volumeDir)},
		{Name: envVolMode, Value: string(o.Mode)},
		{Name: envVolSize, Value: strconv.FormatInt(o.SizeInBytes, 10)},
	}

	// use different name for helper pods
	helperPod.Name = (helperPod.Name + "-" + string(action) + "-" + o.Name)
	if len(helperPod.Name) > HelperPodNameMaxLength {
		helperPod.Name = helperPod.Name[:HelperPodNameMaxLength]
	}
	helperPod.Namespace = p.namespace
	helperPod.Spec.NodeName = o.Node
	helperPod.Spec.ServiceAccountName = p.serviceAccountName
	helperPod.Spec.RestartPolicy = v1.RestartPolicyNever
	helperPod.Spec.Tolerations = append(helperPod.Spec.Tolerations, lpvTolerations...)
	helperPod.Spec.Volumes = append(helperPod.Spec.Volumes, lpvVolumes...)
	helperPod.Spec.Containers[0].Command = cmd
	helperPod.Spec.Containers[0].Env = append(helperPod.Spec.Containers[0].Env, env...)
	helperPod.Spec.Containers[0].Args = []string{"-p", filepath.Join(parentDir, volumeDir),
		"-s", strconv.FormatInt(o.SizeInBytes, 10),
		"-m", string(o.Mode)}

	// If it already exists due to some previous errors, the pod will be cleaned up later automatically
	logrus.Infof("create the helper pod %s into %s", helperPod.Name, p.namespace)
	_, err = p.kubeClient.CoreV1().Pods(p.namespace).Create(helperPod)
	if err != nil && !k8serror.IsAlreadyExists(err) {
		return err
	}

	defer func() {
		e := p.kubeClient.CoreV1().Pods(p.namespace).Delete(helperPod.Name, &metav1.DeleteOptions{})
		if e != nil {
			logrus.Errorf("unable to delete the helper pod: %v", e)
		}
	}()

	completed := false
	for i := 0; i < p.config.CmdTimeoutSeconds; i++ {
		if pod, err := p.kubeClient.CoreV1().Pods(p.namespace).Get(helperPod.Name, metav1.GetOptions{}); err != nil {
			return err
		} else if pod.Status.Phase == v1.PodSucceeded {
			completed = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !completed {
		return fmt.Errorf("create process timeout after %v seconds", p.config.CmdTimeoutSeconds)
	}

	logrus.Infof("Volume %v has been %vd on %v:%v", o.Name, action, o.Node, o.Path)
	return nil
}

func addVolumeMount(mounts *[]v1.VolumeMount, name, mountPath string) *v1.VolumeMount {
	for i, m := range *mounts {
		if m.Name == name {
			if m.MountPath == "" {
				(*mounts)[i].MountPath = mountPath
			}
			return &(*mounts)[i]
		}
	}
	*mounts = append(*mounts, v1.VolumeMount{Name: name, MountPath: mountPath})
	return &(*mounts)[len(*mounts)-1]
}

func isJSONFile(configFile string) bool {
	return strings.HasSuffix(configFile, ".json")
}

func unmarshalFromString(configFile string) (*ConfigData, error) {
	var data ConfigData
	if err := json.Unmarshal([]byte(configFile), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func loadConfigFile(configFile string) (cfgData *ConfigData, err error) {
	defer func() {
		err = errors.Wrapf(err, "fail to load config file %v", configFile)
	}()

	if !isJSONFile(configFile) {
		return unmarshalFromString(configFile)
	}

	f, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var data ConfigData
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func canonicalizeConfig(data *ConfigData) (cfg *Config, err error) {
	defer func() {
		err = errors.Wrapf(err, "config canonicalization failed")
	}()
	cfg = &Config{}
	cfg.NodePathMap = map[string]*NodePathMap{}
	for nodename, n := range data.NodePathMap {
		if cfg.NodePathMap[nodename] != nil {
			return nil, fmt.Errorf("duplicate node %v", nodename)
		}

		if n.PreserveVolum == 0 {
			// Default 10G
			n.PreserveVolum = 10 * 1024
		}

		npMap := &NodePathMap{PreserveVolum: n.PreserveVolum,Paths: map[string]*Disk{}}
		cfg.NodePathMap[nodename] = npMap
		for _, p := range n.Paths {
			if p[0] != '/' {
				return nil, fmt.Errorf("path must start with / for path %v on node %v", p, nodename)
			}
			path, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			if path == "/" {
				return nil, fmt.Errorf("cannot use root ('/') as path on node %v", nodename)
			}
			if _, ok := npMap.Paths[path]; ok {
				return nil, fmt.Errorf("duplicate path %v on node %v", p, nodename)
			}

			// 更新disk信息
			npMap.Paths[path] = &Disk{
			}
		}
	}

	// 将每个节点磁盘信息写到NodePathMap


	if data.CmdTimeoutSeconds > 0 {
		cfg.CmdTimeoutSeconds = data.CmdTimeoutSeconds
	} else {
		cfg.CmdTimeoutSeconds = defaultCmdTimeoutSeconds
	}
	return cfg, nil
}
