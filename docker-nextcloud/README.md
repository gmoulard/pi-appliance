

see: #https://ownyourbits.com/2017/06/08/nextcloudpi-docker-for-raspberry-pi/

```

# Start


docker stop nextcloudpi ; docker rm nextcloudpi
#docker volume rm ncdata
docker run -d -p 3443:443 \
              -p 4443:4443 \
              -p 8080:80 \
              -v ncdata:/data \
              -v /var/www/html/DD:/DD  \
              --restart=always \
              --name nextcloudpi ownyourbits/nextcloudpi 127.0.0.1 pi3b.moulard.org 192.168.1.49
ou

docker run -d -p 3443:443 \
              -p 4443:4443 \
              -p 8080:80 \
              -v ncdata:/data \
              -v /var/www/html/DD:/DD  \
              --restart=always \
              --name nextcloudpi ownyourbits/nextcloudpi-armhf 127.0.0.1 pi3b.moulard.org 192.168.1.49
   
wget https://raw.githubusercontent.com/gmoulard/pi-appliance/master/docker-nextcloud/data/app/config/config.php
docker exec -it nextcloudpi mv /data/app/config/config.php /data/app/config/config.php.ori
docker cp config.php nextcloudpi:/data/app/config/
docker exec -it nextcloudpi chown www-data:www-data /data/app/config/config.php


              
              
# reset password
docker exec -it nextcloudpi sudo -u www-data /var/www/nextcloud/occ user:resetpassword admin

``` 
