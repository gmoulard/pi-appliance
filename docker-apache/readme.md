

Merci gwendal : https://github.com/gwendal-orinel/docker


* build images Apache
$ docker build https://github.com/gmoulard/pi-appliance.git#master:docker-apache -f dockerfile.php.mariadb -t pi-appliance-www

$ docker run -d --name=pi-appliance-www -h pi-appliance-www --restart=always -v /var/log:/var/logc -v /var/www:/var/www -v  letsencrypt:/etc/letsencrypt -p 443:443 -p 80:80  pi-appliance-www


initialiasation des certificat SSL 
$ docker exec -it pi-appliance-www bash /certbot-auto --apache  -n --email gmoulard@gmail.com --agree-tos -d pi3.moulard.org  -d pi3b.moulard.org

renouvellement certificat : 
$ docker exec -it pi-appliance-www bash /certbot-auto renew


