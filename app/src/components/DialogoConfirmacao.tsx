interface Props {
  titulo: string
  mensagem: string
  confirmarLabel: string
  carregando?: boolean
  onConfirmar: () => void
  onCancelar: () => void
}

export function DialogoConfirmacao({
  titulo,
  mensagem,
  confirmarLabel,
  carregando,
  onConfirmar,
  onCancelar,
}: Props) {
  return (
    <div className="dialog-overlay" onMouseDown={onCancelar}>
      <div
        className="dialog"
        role="dialog"
        aria-modal="true"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <h2 className="dialog-titulo">{titulo}</h2>
        <p className="dialog-mensagem">{mensagem}</p>
        <div className="dialog-acoes">
          <button type="button" className="btn" onClick={onCancelar} disabled={carregando}>
            Cancelar
          </button>
          <button type="button" className="btn btn-danger" onClick={onConfirmar} disabled={carregando}>
            {confirmarLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
