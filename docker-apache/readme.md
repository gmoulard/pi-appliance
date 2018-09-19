

Merci gwendal : https://github.com/gwendal-orinel/docker/blob/master/proxy/Dockerfile 



docker build https://github.com/gmoulard/pi-appliance.git#master:docker-apache -f dockerfile.php.mariadb -t pi-appliance-www

docker run -d --name=pi-appliance-www \
           -h pi-appliance-www \
           --restart=always \
           -v /var/log:/var/log \
           -v /var/www:/var/w \
           -p 443:443 -p 80:80 \
           pi-appliance-www
