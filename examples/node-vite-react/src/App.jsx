import { useState } from "react";

function App() {
  const [count, setCount] = useState(0);

  return (
    <div>
      <h1>Vite + React</h1>
      <button onClick={() => setCount((c) => c + 1)}>Count: {count}</button>
      <p>Edit src/App.jsx and save to test HMR.</p>
    </div>
  );
}

export default App;
