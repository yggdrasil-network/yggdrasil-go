.PHONY: all

all: util yggdrasil-brute-multi-curve25519 yggdrasil-brute-multi-ed25519

util: util.c
	gcc -Wall -std=c89 -O3 -c -o util.o util.c

yggdrasil-brute-multi-ed25519: yggdrasil-brute-multi-ed25519.c util.o
	gcc -Wall -std=c89 -O3 -o yggdrasil-brute-multi-ed25519 -lsodium yggdrasil-brute-multi-ed25519.c util.o

yggdrasil-brute-multi-curve25519: yggdrasil-brute-multi-curve25519.c util.o
	gcc -Wall -std=c89 -O3 -o yggdrasil-brute-multi-curve25519 -lsodium yggdrasil-brute-multi-curve25519.c util.o
