FROM resin/rpi-raspbian:latest

RUN apt-get update
RUN apt-get upgrade -y
#RUN apt-get dist-upgrade

RUN apt-get -qy install curl \
                        vim \
                        apache2 

RUN  a2ensite default-ssl.conf

EXPOSE 80
EXPOSE 443
RUN source /etc/apache2/envvars
ENTRYPOINT ["exec apache2 -D FOREGROUND"]

