import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { LoopZeroUIProvider } from '@loopzero/ui';
import App from './app';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LoopZeroUIProvider>
      <App />
    </LoopZeroUIProvider>
  </StrictMode>
);
