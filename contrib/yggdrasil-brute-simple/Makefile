.PHONY: all

all: util mesh-brute-multi-curve25519 mesh-brute-multi-ed25519

util: util.c
	gcc -Wall -std=c89 -O3 -c -o util.o util.c

mesh-brute-multi-ed25519: mesh-brute-multi-ed25519.c util.o
	gcc -Wall -std=c89 -O3 -o mesh-brute-multi-ed25519 -lsodium mesh-brute-multi-ed25519.c util.o

mesh-brute-multi-curve25519: mesh-brute-multi-curve25519.c util.o
	gcc -Wall -std=c89 -O3 -o mesh-brute-multi-curve25519 -lsodium mesh-brute-multi-curve25519.c util.o
