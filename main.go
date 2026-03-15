package main

import (
	"github.com/sirupsen/logrus"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"

	sftpstore "github.com/freshost/velero-plugin-for-sftp/sftp"
)

func main() {
	veleroplugin.NewServer().
		RegisterObjectStore("velero.io/sftp", newSFTPObjectStore).
		Serve()
}

func newSFTPObjectStore(logger logrus.FieldLogger) (interface{}, error) {
	return sftpstore.NewObjectStore(logger), nil
}
