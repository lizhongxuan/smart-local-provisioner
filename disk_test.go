package main

import (
	"reflect"
	pkgexec "storage-provisioner/util/exec"
	"storage-provisioner/util/sys"
	"testing"
)

func Test_probeDevices(t *testing.T) {
	type args struct {
		executor pkgexec.Executor
	}
	tests := []struct {
		name    string
		args    args
		want    []sys.LocalDisk
		wantErr bool
	}{
		{"test1",args{executor: &pkgexec.CommandExecutor{}},nil,false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := probeDevices( tt.args.executor)
			if (err != nil) != tt.wantErr {
				t.Errorf("probeDevices() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("probeDevices() got = %v, want %v", got, tt.want)
			}
			t.Log(got)
		})
	}
}
