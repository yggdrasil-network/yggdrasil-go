#include "yggdrasil-brute.h"

int find_where(unsigned char hash[64], unsigned char besthashlist[NUMKEYS][64]) {
	/* Where to insert hash into sorted hashlist */
	int j;
	int where = -1;
	for (j = 0; j < NUMKEYS; ++j) {
		if (memcmp(hash, besthashlist[j], 64) > 0) ++where;
		else break;
	}
	return where;
}

void insert_64(unsigned char itemlist[NUMKEYS][64], unsigned char item[64], int where) {
	int j;
	for (j = 0; j < where; ++j) {
		memcpy(itemlist[j], itemlist[j+1], 64);
	}
	memcpy(itemlist[where], item, 64);
}

void insert_32(unsigned char itemlist[NUMKEYS][32], unsigned char item[32], int where) {
	int j;
	for (j = 0; j < where; ++j) {
		memcpy(itemlist[j], itemlist[j+1], 32);
	}
	memcpy(itemlist[where], item, 32);
}

void make_addr(unsigned char addr[32], unsigned char hash[64]) {
	/* Public key hash to yggdrasil ipv6 address */
	int i;
	int offset;
	unsigned char mask;
	unsigned char c;
	int ones = 0;
	unsigned char br = 0; /* false */
	for (i = 0; i < 64 && !br; ++i) {
		mask = 128;
		c = hash[i];
		while (mask) {
			if (c & mask) {
				++ones;
			} else {
				br = 1; /* true */
				break;
			}
			mask >>= 1;
		}
	}

	addr[0] = 2;
	addr[1] = ones;

	offset = ones + 1;
	for (i = 0; i < 14; ++i) {
		c = hash[offset/8] << (offset%8);
		c |= hash[offset/8 + 1] >> (8 - offset%8);
		addr[i + 2] = c;
		offset += 8;
	}
}
