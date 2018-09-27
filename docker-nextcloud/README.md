

see: #https://ownyourbits.com/2017/06/08/nextcloudpi-docker-for-raspberry-pi/

```
mkdir /data
mkdir /data/config
sudo wget https://github.com/gmoulard/pi-appliance/raw/master/docker-nextcloud/data/config/config.php \
          -O /data/config/config.php

# Start
docker stop nextcloud ; docker rm nextcloud

docker run -d -p 443:443 -p 80:80 -p 4443:4443  -v ncdata:/data --name nextcloudpi ownyourbits/nextcloudpi 127.0.0.1

docker run -d -p 1443:443 \
              -p 180:80 \
              -p 4443:4443 \
              -v /data:/data \
              -v /media:/media  \
              --restart unless-stopped  \
              --name nextcloud \
              --hostname nextcloud \
              ownyourbits/nextcloudpi pi3b.moulard.org 192.168.1.49

# reset password
docker exec -it nextcloudpi sudo -u www-data /var/www/nextcloud/occ user:resetpassword admin

``` 
