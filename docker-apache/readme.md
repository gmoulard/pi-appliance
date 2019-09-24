

Merci [@gwendal-orinel](https://github.com/gwendal-orinel/docker)


# build images Apache 
```
#Build
docker build https://github.com/gmoulard/pi-appliance.git#master:docker-apache \
               -f dockerfile.php.mariadb -t pi-appliance-www

#Start
docker stop pi-appliance-www; docker rm pi-appliance-www
WHOSTNAME=www.pi-appliance.com
docker run -d --name=pi-appliance-www \
              --restart=always \
              -v /var/log:/var/log \
              -v /var/www:/var/www  \
              -v pi-appliance-www_etc_letsencrypt:/etc/letsencrypt \
              -v pi-appliance-www_lib_letsencrypt:/var/lib/letsencrypt \
              --hostname $WHOSTNAME \
              -p 443:443 \
              -p 80:80  \
              pi-appliance-www

#pi3
docker exec -it pi-appliance-www a2ensite pi3.moulard.org.conf  pi3.moulard.org-ssl.conf martin.moulard.org

#pi3b
docker exec -it pi-appliance-www a2ensite pi3b.moulard.org.conf pi3b.moulard.org-ssl.conf 

#vgm 
docker exec -it pi-appliance-www a2ensite pi-appliance.com.conf pi-appliance.com-ssl.conf

```

# initialiasation des certificat SSL 
```
#Init
docker exec -it pi-appliance-www wget https://dl.eff.org/certbot-auto
docker exec -it pi-appliance-www chmod a+x certbot-auto

#request pi3
docker exec -it pi-appliance-www /certbot-auto --apache  -n \
            --email gmoulard@gmail.com --agree-tos \
            -d pi3.moulard.org  \
            -d pi3b.moulard.org \
            -d cyprin.eu \
            -d tom.moulard.org \
            -d couill.eu

            -d traefik.cyprin.eu  \
            -d git.cyprin.eu 
            -d torent.cyprin.eu \
            -d video.cyprin.eu \
            -d jupiter.cyprin.eu \
            -d paste.cyprin.eu \
            -d cloud.cyprin.eu \
            -d latex.cyprin.eu \


#request pi3b
docker exec -it pi-appliance-www /certbot-auto --apache -n \
            --email gmoulard@gmail.com --agree-tos \
            -d pi3b.moulard.org

#request vgm
docker exec -it pi-appliance-www /certbot-auto --apache -n \
            --email gmoulard@gmail.com --agree-tos \
            -d pi-appliance.com

renew 
 docker exec -it pi-appliance-www /certbot-auto renew
```
