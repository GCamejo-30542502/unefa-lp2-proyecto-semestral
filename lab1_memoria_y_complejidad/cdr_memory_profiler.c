#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#define RECORD_COUNT UINT64_C(10000000)
#define CACHE_LINE_BYTES ((size_t)64)

typedef struct CDR_Desalineado {
    char tipo_red;
    uint64_t imsi_abonado;
    char bandera_roaming;
    uint32_t celda_id;
    char codigo_estado;
    uint16_t puerto_origen;
} CDR_Desalineado;

struct CDR_Optimizado {
    uint64_t imsi_abonado;
    uint32_t celda_id;
    uint16_t puerto_origen;
    char tipo_red;
    char bandera_roaming;
    char codigo_estado;
};

static uint64_t global_data = UINT64_C(500123456789012);

#define PRINT_FIELD_OFFSET(type, field) \
    printf("  %-20s %zu\n", #field, offsetof(type, field))

#define PRINT_LAYOUT(type)                                      \
    do {                                                        \
        printf("\n%s: sizeof = %zu bytes\n", #type, sizeof(type)); \
        PRINT_FIELD_OFFSET(type, tipo_red);                     \
        PRINT_FIELD_OFFSET(type, imsi_abonado);                 \
        PRINT_FIELD_OFFSET(type, bandera_roaming);              \
        PRINT_FIELD_OFFSET(type, celda_id);                     \
        PRINT_FIELD_OFFSET(type, codigo_estado);                 \
        PRINT_FIELD_OFFSET(type, puerto_origen);                 \
    } while (0)

int main(void)
{
    int stack_local = 42;
    void *heap_block = malloc(64U);
    uint64_t bytes_saved;
    double decimal_megabytes;
    double mebibytes;

    if (heap_block == NULL) {
        fprintf(stderr, "Error: no se pudo reservar memoria en el heap.\n");
        return EXIT_FAILURE;
    }

    printf("Direcciones virtuales (la ubicacion puede variar entre ejecuciones):\n");
    printf("  Global (.data): %p\n", (void *)&global_data);
    printf("  Local (stack):  %p\n", (void *)&stack_local);
    printf("  Bloque (heap):  %p\n", heap_block);

    PRINT_LAYOUT(struct CDR_Desalineado);
    PRINT_LAYOUT(struct CDR_Optimizado);

    bytes_saved = (uint64_t)(sizeof(struct CDR_Desalineado) -
                             sizeof(struct CDR_Optimizado)) * RECORD_COUNT;
    decimal_megabytes = (double)bytes_saved / 1000000.0;
    mebibytes = (double)bytes_saved / (1024.0 * 1024.0);

    printf("\nLote analizado: %llu registros\n",
           (unsigned long long)RECORD_COUNT);
    printf("Ahorro al usar CDR_Optimizado: %llu bytes (%.2f MB; %.2f MiB)\n",
           (unsigned long long)bytes_saved, decimal_megabytes, mebibytes);
    printf("Registros completos por linea L1 de %zu bytes:\n",
           CACHE_LINE_BYTES);
    printf("  CDR_Desalineado: %zu\n",
           CACHE_LINE_BYTES / sizeof(struct CDR_Desalineado));
    printf("  CDR_Optimizado:  %zu\n",
           CACHE_LINE_BYTES / sizeof(struct CDR_Optimizado));

    free(heap_block);
    heap_block = NULL;
    printf("\nBloque del heap liberado con free().\n");

    return EXIT_SUCCESS;
}
