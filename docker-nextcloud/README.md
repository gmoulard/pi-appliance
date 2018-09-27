

see: #https://ownyourbits.com/2017/06/08/nextcloudpi-docker-for-raspberry-pi/

```
mkdir /data
mkdir /data/config
sudo wget https://github.com/gmoulard/pi-appliance/raw/master/docker-nextcloud/data/config/config.php \
          -O /data/config/config.php

# Start
docker stop nextcloudpi ; docker rm nextcloudpi

docker run -d -p 3443:443 \
              -v ncdata:/data \
              -v /media:/media  \
              --restart=always \
              --name nextcloudpi ownyourbits/nextcloudpi 127.0.0.1 pi3b.moulard.org 192.168.1.49
   
wget https://raw.githubusercontent.com/gmoulard/pi-appliance/master/docker-nextcloud/data/config/config.php
docker cp config.php nextcloudpi:/data/config
docker exec -it nextcloudpi chown www-data:www-data /data/config/config.php
#docker exec -it nextcloudpi chmod 666 /data/config/config.php

              
              
# reset password
docker exec -it nextcloudpi sudo -u www-data /var/www/nextcloud/occ user:resetpassword admin

``` 
