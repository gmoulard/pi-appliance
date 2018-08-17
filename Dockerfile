FROM scratch
COPY ./* /pi-appliance/
RUN /pi-appliance/start.sh
