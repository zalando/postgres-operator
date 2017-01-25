FROM alpine
ADD postgres-operator /usr/local/bin

CMD ["/usr/local/bin/postgres-operator"] 

