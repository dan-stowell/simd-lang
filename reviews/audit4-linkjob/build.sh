#!/bin/sh
# Run from repository root, only after shared stage2 build has completed.
set -eu
mkdir -p build/audit4-linkjob
python3 reviews/audit4-linkjob/make_byte_store.py
gcc -O2 -o build/audit4-linkjob/ref_o2 explore/linkjob.c
gcc -O2 -o build/audit4-linkjob/ref_o2_bytes reviews/audit4-linkjob/ref_bytestore.c
gcc -O3 -march=native -o build/audit4-linkjob/ref_o3 explore/linkjob.c
gcc -O2 -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=0 -o build/audit4-linkjob/ref_o2_nf explore/linkjob.c
gcc -O3 -march=native -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=0 -o build/audit4-linkjob/ref_o3_nf explore/linkjob.c
gcc -O3 -march=native -fopenmp -Wall -Wextra -o build/audit4-linkjob/variants reviews/audit4-linkjob/variants.c
gcc -O3 -march=native -fopenmp -Wall -Wextra -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=0 -o build/audit4-linkjob/variants_nf reviews/audit4-linkjob/variants.c
for name in link link2; do
    build/stage2 < examples/"$name".strand > build/audit4-linkjob/"$name"
    chmod +x build/audit4-linkjob/"$name"
done
sha256sum build/stage2 build/audit4-linkjob/ref_o2 build/audit4-linkjob/ref_o2_bytes build/audit4-linkjob/ref_o3 build/audit4-linkjob/ref_o2_nf build/audit4-linkjob/ref_o3_nf build/audit4-linkjob/variants build/audit4-linkjob/variants_nf build/audit4-linkjob/link build/audit4-linkjob/link2
