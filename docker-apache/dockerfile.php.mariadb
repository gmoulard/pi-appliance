FROM debian:latest

RUN apt-get update -y && apt-get install -qy wget vim apache2 libapache2-mod-php php php-xml php7.0-mbstring php-curl 

RUN apt-get update -y
RUN apt-get -qy install apt-utils

RUN apt-get -qy install curl \
                        vim \
                        php \
                        libapache2-mod-php \
                        php-xml \
                        php7.0-mbstring php-curl \
                        apache2 
                        
RUN apt-get -qy install mariadb-client
RUN apt-get -qy install php-gd
RUN apt-get -qy install mcrypt

RUN a2enmod proxy proxy_http proxy_ajp rewrite deflate headers proxy_balancer proxy_connect proxy_html rewrite
RUN a2ensite default-ssl.conf

# Manually set up the apache environment variables
ENV APACHE_RUN_USER www-data
ENV APACHE_RUN_GROUP www-data
ENV APACHE_LOG_DIR /var/log/apache2
ENV APACHE_LOCK_DIR /var/lock/apache2
ENV APACHE_PID_FILE /var/run/apache2.pid

EXPOSE 80
EXPOSE 443

COPY etc/* /etc

CMD /usr/sbin/apache2ctl -D FOREGROUND
