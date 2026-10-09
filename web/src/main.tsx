import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { Live } from './state/live'
import { LiveProvider } from './state/context'
import './styles/theme.css'
import './styles/app.css'

const live = new Live()

const root = document.getElementById('root')
if (!root) throw new Error('no #root element')

createRoot(root).render(
  <StrictMode>
    <LiveProvider live={live}>
      <App />
    </LiveProvider>
  </StrictMode>,
)
