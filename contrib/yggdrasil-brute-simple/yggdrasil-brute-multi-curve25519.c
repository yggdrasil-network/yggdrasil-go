/*
sk: 32 random bytes
sk[0] &= 248;
sk[31] &= 127;
sk[31] |= 64;

increment sk
pk = curve25519_scalarmult_base(mysecret)
hash = sha512(pk)

if besthash:
	bestsk = sk
	besthash = hash
*/

#include "yggdrasil-brute.h"


void seed(unsigned char sk[32]) {
	randombytes_buf(sk, 32);
	sk[0] &= 248;
	sk[31] &= 127;
	sk[31] |= 64;
}


int main(int argc, char **argv) {
	int i;
	int j;
	unsigned char addr[16];
	time_t starttime;
	time_t requestedtime;

	unsigned char bestsklist[NUMKEYS][32];
	unsigned char bestpklist[NUMKEYS][32];
	unsigned char besthashlist[NUMKEYS][64];

	unsigned char sk[32];
	unsigned char pk[32];
	unsigned char hash[64];

	unsigned int runs = 0;
	int where;

	if (argc != 2) {
		fprintf(stderr, "usage: ./yggdrasil-brute-multi-curve25519 <seconds>\n");
		return 1;
	}

	if (sodium_init() < 0) {
		/* panic! the library couldn't be initialized, it is not safe to use */
		printf("sodium init failed!\n");
		return 1;
	}

	starttime = time(NULL);
	requestedtime = atoi(argv[1]);

	if (requestedtime < 0) requestedtime = 0;
	fprintf(stderr, "Searching for yggdrasil curve25519 keys (this will take slightly longer than %ld seconds)\n", requestedtime);

	sodium_memzero(bestsklist, NUMKEYS * 32);
	sodium_memzero(bestpklist, NUMKEYS * 32);
	sodium_memzero(besthashlist, NUMKEYS * 64);
	seed(sk);

	do {
		/* generate pubkey, hash, compare, increment secret.
		 * this loop should take 4 seconds on modern hardware */
		for (i = 0; i < (1 << 16); ++i) {
			++runs;
			if (crypto_scalarmult_curve25519_base(pk, sk) != 0) {
				printf("scalarmult to create pub failed!\n");
				return 1;
			}
			crypto_hash_sha512(hash, pk, 32);

			where = find_where(hash, besthashlist);
			if (where >= 0) {
				insert_32(bestsklist, sk, where);
				insert_32(bestpklist, pk, where);
				insert_64(besthashlist, hash, where);

				seed(sk);
			}
			for (j = 1; j < 31; ++j) if (++sk[j]) break;
		}
	} while (time(NULL) - starttime < requestedtime || runs < NUMKEYS);

	fprintf(stderr, "--------------addr-------------- -----------------------------secret----------------------------- -----------------------------public-----------------------------\n");
	for (i = 0; i < NUMKEYS; ++i) {
		make_addr(addr, besthashlist[i]);
		for (j = 0; j < 16; ++j) printf("%02x", addr[j]);
		printf(" ");
		for (j = 0; j < 32; ++j) printf("%02x", bestsklist[i][j]);
		printf(" ");
		for (j = 0; j < 32; ++j) printf("%02x", bestpklist[i][j]);
		printf("\n");
	}

	sodium_memzero(bestsklist, NUMKEYS * 32);
	sodium_memzero(sk, 32);

	return 0;
}
