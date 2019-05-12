/*
seed: 32 random bytes
sk: sha512(seed)
sk[0] &= 248
sk[31] &= 127
sk[31] |= 64

pk: scalarmult_ed25519_base(sk)


increment seed
generate sk
generate pk
hash = sha512(mypub)

if besthash:
	bestseed = seed
	bestseckey = sk
	bestpubkey = pk
	besthash = hash
*/

#include "yggdrasil-brute.h"


int main(int argc, char **argv) {
	int i;
	int j;
	time_t starttime;
	time_t requestedtime;

	unsigned char bestsklist[NUMKEYS][64]; /* sk contains pk */
	unsigned char besthashlist[NUMKEYS][64];

	unsigned char seed[32];
	unsigned char sk[64];
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
	fprintf(stderr, "Searching for yggdrasil ed25519 keys (this will take slightly longer than %ld seconds)\n", requestedtime);

	sodium_memzero(bestsklist, NUMKEYS * 64);
	sodium_memzero(besthashlist, NUMKEYS * 64);
	randombytes_buf(seed, 32);

	do {
		/* generate pubkey, hash, compare, increment secret.
		 * this loop should take 4 seconds on modern hardware */
		for (i = 0; i < (1 << 17); ++i) {
			++runs;
			crypto_hash_sha512(sk, seed, 32);

			if (crypto_scalarmult_ed25519_base(pk, sk) != 0) {
				printf("scalarmult to create pub failed!\n");
				return 1;
			}
			memcpy(sk + 32, pk, 32);

			crypto_hash_sha512(hash, pk, 32);

			/* insert into local list of good key */
			where = find_where(hash, besthashlist);
			if (where >= 0) {
				insert_64(bestsklist, sk, where);
				insert_64(besthashlist, hash, where);
				randombytes_buf(seed, 32);
			}
			for (j = 1; j < 31; ++j) if (++seed[j]) break;
		}
	} while (time(NULL) - starttime < requestedtime || runs < NUMKEYS);

	fprintf(stderr, "!! Secret key is seed concatenated with public !!\n");
	fprintf(stderr, "---hash--- ------------------------------seed------------------------------ -----------------------------public-----------------------------\n");
	for (i = 0; i < NUMKEYS; ++i) {
		for (j = 0; j < 5; ++j) printf("%02x", besthashlist[i][j]);
		printf(" ");
		for (j = 0; j < 32; ++j) printf("%02x", bestsklist[i][j]);
		printf(" ");
		for (j = 32; j < 64; ++j) printf("%02x", bestsklist[i][j]);
		printf("\n");
	}

	sodium_memzero(bestsklist, NUMKEYS * 64);
	sodium_memzero(sk, 64);
	sodium_memzero(seed, 32);

	return 0;
}
