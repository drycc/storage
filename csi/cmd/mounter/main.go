package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/drycc/storage/csi/driver"
	"github.com/drycc/storage/csi/mounter"
)

var usage = `
Mounter a s3 bucket.

Usage:
  mounter <command> --options <options>

Arguments:
  <command>
    mounter sub command, supports: [mount,quota,unmount]
  <options>
    base64 encrypted options json string, example:
    {
		"mounter": "seaweedfs",
		"bucket": {
			"name": "bucket name",
			"prefix": "s3 bucket prefix",
			"endpoint": "s3 endpoint",
			"capacity": "bucket capacity",
			"secrets": "bucket secrets"
		},
		"target": "/xxx/volumes/kubernetes.io~csi/pvc-xxx/mount",
		"options": []
	}

For more details see: https://github.com/drycc/storage/blob/main/csi/cmd/mounter.go
`

func main() {
	if len(os.Args) != 4 {
		fmt.Println(usage)
		os.Exit(1)
	}
	command := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)
	optionsData := flag.String("options", "", "the options for mounter")
	flag.Parse()
	optionsJson, err := base64.StdEncoding.DecodeString(*optionsData)
	if err != nil {
		fmt.Println(usage)
		log.Fatal(err)
	}
	var options driver.MounterOptions
	if json.Unmarshal(optionsJson, &options); err != nil {
		fmt.Println(usage)
		log.Fatal(err)
	}

	mounter, err := mounter.GetMounter(options.Mounter)
	if err != nil {
		log.Fatal(err)
	}
	if command == "mount" {
		err = mounter.Mount(options.Bucket, options.Target, options.Options)
	} else if command == "quota" {
		err = mounter.Quota(options.Bucket)
	} else if command == "unmount" {
		err = mounter.Unmount(options.Target)
	} else {
		fmt.Println(usage)
		os.Exit(1)
	}
	if err != nil {
		log.Fatal(err)
	}
}
