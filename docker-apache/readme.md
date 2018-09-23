

Merci [@gwendal-orinel](https://github.com/gwendal-orinel/docker)


# build images Apache 
```
Build
$ docker build https://github.com/gmoulard/pi-appliance.git#master:docker-apache -f dockerfile.php.mariadb -t pi-appliance-www

Start
docker stop pi-appliance-www; docker rm pi-appliance-www
docker run -d --name=pi-appliance-www -h pi-appliance-www --restart=always \
             -v /var/log:/var/log \
             -v /var/www:/var/www -v  \
             letsencrypt:/etc/letsencrypt -p 443:443 -p 80:80  pi-appliance-www

```

# initialiasation des certificat SSL 
```
Request
PI3
$ docker exec -it pi-appliance-www bash /certbot-auto --apache  -n --email gmoulard@gmail.com --agree-tos -d pi3.moulard.org  -d pi3b.moulard.org
PI3b
$ d exec -it pi-appliance-www bash /certbot-auto --apache --email gmoulard@gmail.com --agree-tos -d pi3b.moulard.org

renew 
$ docker exec -it pi-appliance-www bash /certbot-auto renew
```
