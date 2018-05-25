#!/bin/sh



#wget https://github.com/ovh/svfs/releases/download/v0.9.1/svfs_0.9.1_amd64.deb
#dpkg -i svfs_0.9.1_amd64.deb



export OS_USERNAME=guillaume.moulard@orange.com
export OS_PASSWORD=azeAZE123123123
export OS_TENANT_NAME=0750177459_Common_Bundle
export OS_REGION_NAME=fr1
export OS_AUTH_URL=https://identity.fr1.cloudwatt.com/v2.0

sudo mount -t svfs -o username=$OS_USERNAME,password=$OS_PASSWORD,tenant=$OS_TENANT_NAME,region=$OS_REGION_NAME,auth_url=$OS_AUTH_URL cloud.moulard.org.DontDrop /mnt/swiftCloudwatt

