

see: #https://ownyourbits.com/2017/06/08/nextcloudpi-docker-for-raspberry-pi/

```
# Start
DOMAIN="pi3b.moulard.org 192.168.1.49"
docker run -d -p 1443:443 -p 180:80 -v ncdata:/data -v /media:/media  --restart unless-stopped  --name nextcloudpi ownyourbits/nextcloudpi $DOMAINE

# reset password
docker exec -it nextcloudpi sudo -u www-data /var/www/nextcloud/occ user:resetpassword admin

``` 
