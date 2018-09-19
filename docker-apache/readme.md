

Merci gwendal : https://github.com/gwendal-orinel/docker/blob/master/proxy/Dockerfile 



docker build https://github.com/gmoulard/pi-appliance.git#master:docker-apache -f dockerfile.php.mariadb -t pi-appliance-www
docker run -d --name=apache2 -h apache2 --restart=always -p 1443:443 -p 80:80 apache2
