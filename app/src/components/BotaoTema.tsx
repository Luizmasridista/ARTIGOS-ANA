import { IconeLua, IconeSol } from './Icones'
import type { Tema } from '../tema'

interface Props {
  tema: Tema
  onAlternar: () => void
}

export function BotaoTema({ tema, onAlternar }: Props) {
  return (
    <button
      type="button"
      className="btn btn-icon"
      onClick={onAlternar}
      title={tema === 'dark' ? 'Mudar para tema claro' : 'Mudar para tema escuro'}
      aria-label="Alternar tema"
    >
      {tema === 'dark' ? <IconeSol /> : <IconeLua />}
    </button>
  )
}
