package domain

import (
	"errors"
	"regexp"
)

// Estándar ITU-T E.164: "+" seguido de un dígito 1-9 y de 6 a 14 dígitos más.
var e164 = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

// Errores de dominio (el exterior los puede comparar con errors.Is)
var (
	ErrCamposObligatorios = errors.New("id e idempKey son obligatorios")
	ErrMSISDNInvalido     = errors.New("msisdn viola el estándar ITU-T E.164")
	ErrMontoInvalido      = errors.New("el monto debe ser estrictamente positivo (en centavos)")
	ErrTransicionInvalida = errors.New("transición de estado no permitida")
)

// Estado modela la máquina de estados de la transacción.
type Estado string

const (
	Pendiente Estado = "PENDIENTE"
	Aprobada  Estado = "APROBADA"
	Rechazada Estado = "RECHAZADA"
)

// TransaccionRecarga es la entidad. Sus campos son privados (minúscula)
// para que nadie pueda saltarse las validaciones modificándolos directamente.
type TransaccionRecarga struct {
	id            string
	idempKey      string
	msisdn        string
	montoCentavos int64 // entero: prohibido float64 para dinero
	estado        Estado
}

// NuevaTransaccionRecarga es el constructor: solo devuelve una transacción
// si cumple todas las reglas. Siempre nace en estado PENDIENTE.
func NuevaTransaccionRecarga(id, idempKey, msisdn string, montoCentavos int64) (*TransaccionRecarga, error) {
	if id == "" || idempKey == "" {
		return nil, ErrCamposObligatorios
	}
	// (1) Numeración internacional E.164
	if !e164.MatchString(msisdn) {
		return nil, ErrMSISDNInvalido
	}
	// (2) Monto estrictamente positivo
	if montoCentavos <= 0 {
		return nil, ErrMontoInvalido
	}
	return &TransaccionRecarga{
		id:            id,
		idempKey:      idempKey,
		msisdn:        msisdn,
		montoCentavos: montoCentavos,
		estado:        Pendiente,
	}, nil
}

// (3) Máquina de estados: PENDIENTE -> APROBADA | RECHAZADA
// Desde APROBADA o RECHAZADA ya no se puede cambiar (son estados finales).

func (t *TransaccionRecarga) Aprobar() error {
	if t.estado != Pendiente {
		return ErrTransicionInvalida
	}
	t.estado = Aprobada
	return nil
}

func (t *TransaccionRecarga) Rechazar() error {
	if t.estado != Pendiente {
		return ErrTransicionInvalida
	}
	t.estado = Rechazada
	return nil
}

// Getters de solo lectura (el exterior puede consultar, no modificar).
func (t *TransaccionRecarga) ID() string           { return t.id }
func (t *TransaccionRecarga) IdempKey() string     { return t.idempKey }
func (t *TransaccionRecarga) MSISDN() string       { return t.msisdn }
func (t *TransaccionRecarga) MontoCentavos() int64 { return t.montoCentavos }
func (t *TransaccionRecarga) Estado() Estado       { return t.estado }
