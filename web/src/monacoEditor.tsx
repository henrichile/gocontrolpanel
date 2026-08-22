// Envoltorio propio para que Monaco (pesado) quede en su propio chunk y solo
// se descargue cuando de verdad se abre el editor de archivos, no en la
// carga inicial del panel.
import './monacoSetup'
export { default } from '@monaco-editor/react'
