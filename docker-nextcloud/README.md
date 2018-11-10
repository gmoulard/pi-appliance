

see: #https://ownyourbits.com/2017/06/08/nextcloudpi-docker-for-raspberry-pi/

```

# Start
export DOMAIN="192.168.1.53 pi3b.moulard.org"
docker run -d  \
           -p 4443:4443 -p 443:443 -p 80:80 \
           -v ncdata:/data \
           -v /var/www/html/DD:/DD \
           --restart=always \
           --name nextcloudpi ownyourbits/nextcloudpi-armhf $DOMAIN

docker exec -it nextcloudpi bash
add at the and of /data/app/config/config.php
  'overwriteprotocol' => 'https',
  'overwritehost' => 'pi3b.moulard.org',
  'overwritewebroot' => '/cloud',
  'htaccess.RewriteBase' => '/cloud/',

              
              
# reset password
docker exec -it nextcloudpi sudo -u www-data /var/www/nextcloud/occ user:resetpassword admin

``` 
