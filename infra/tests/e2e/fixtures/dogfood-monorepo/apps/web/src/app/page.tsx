'use client';

import { useState, useEffect } from 'react';

interface Task {
  id: string;
  title: string;
  description: string;
  priority: 'low' | 'medium' | 'high';
  completed: boolean;
  createdAt: string;
  completedAt: string | null;
}

const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:3333';

export default function Home() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [title, setTitle] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchTasks();
  }, []);

  async function fetchTasks() {
    try {
      const res = await fetch(`${API_URL}/tasks`);
      if (res.ok) {
        setTasks(await res.json());
      }
    } catch (err) {
      console.error('Failed to fetch tasks:', err);
    } finally {
      setLoading(false);
    }
  }

  async function createTask(e: React.FormEvent) {
    e.preventDefault();
    if (!title.trim()) return;

    try {
      const res = await fetch(`${API_URL}/tasks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title }),
      });
      if (res.ok) {
        const task = await res.json();
        setTasks((prev) => [...prev, task]);
        setTitle('');
      }
    } catch (err) {
      console.error('Failed to create task:', err);
    }
  }

  async function completeTask(id: string) {
    try {
      const res = await fetch(`${API_URL}/tasks/${id}/complete`, { method: 'POST' });
      if (res.ok) {
        const updated = await res.json();
        setTasks((prev) => prev.map((t) => (t.id === id ? updated : t)));
      }
    } catch (err) {
      console.error('Failed to complete task:', err);
    }
  }

  async function deleteTask(id: string) {
    try {
      const res = await fetch(`${API_URL}/tasks/${id}`, { method: 'DELETE' });
      if (res.ok) {
        setTasks((prev) => prev.filter((t) => t.id !== id));
      }
    } catch (err) {
      console.error('Failed to delete task:', err);
    }
  }

  if (loading) return <main style={styles.main}><p>Loading...</p></main>;

  return (
    <main style={styles.main}>
      <h1 style={styles.title}>Dogfood SaaS - Tasks</h1>

      <form onSubmit={createTask} style={styles.form}>
        <input
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="New task..."
          style={styles.input}
        />
        <button type="submit" style={styles.button}>Add</button>
      </form>

      <div style={styles.stats}>
        <span>Total: {tasks.length}</span>
        <span>Done: {tasks.filter((t) => t.completed).length}</span>
        <span>Pending: {tasks.filter((t) => !t.completed).length}</span>
      </div>

      <ul style={styles.list}>
        {tasks.map((task) => (
          <li key={task.id} style={styles.item}>
            <div style={styles.taskContent}>
              <span style={{
                ...styles.taskTitle,
                textDecoration: task.completed ? 'line-through' : 'none',
                opacity: task.completed ? 0.6 : 1,
              }}>
                {task.title}
              </span>
              <span style={styles.priority}>{task.priority}</span>
            </div>
            <div style={styles.actions}>
              {!task.completed && (
                <button onClick={() => completeTask(task.id)} style={styles.actionBtn}>
                  Done
                </button>
              )}
              <button onClick={() => deleteTask(task.id)} style={styles.deleteBtn}>
                Delete
              </button>
            </div>
          </li>
        ))}
      </ul>
    </main>
  );
}

const styles: Record<string, React.CSSProperties> = {
  main: { maxWidth: 640, margin: '0 auto', padding: '2rem', fontFamily: 'system-ui' },
  title: { fontSize: '1.5rem', marginBottom: '1rem' },
  form: { display: 'flex', gap: '0.5rem', marginBottom: '1rem' },
  input: { flex: 1, padding: '0.5rem', border: '1px solid #ccc', borderRadius: 4 },
  button: { padding: '0.5rem 1rem', background: '#0070f3', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer' },
  stats: { display: 'flex', gap: '1rem', marginBottom: '1rem', fontSize: '0.875rem', color: '#666' },
  list: { listStyle: 'none', padding: 0 },
  item: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.75rem', borderBottom: '1px solid #eee' },
  taskContent: { display: 'flex', alignItems: 'center', gap: '0.5rem' },
  taskTitle: { fontSize: '1rem' },
  priority: { fontSize: '0.75rem', padding: '0.125rem 0.5rem', background: '#f0f0f0', borderRadius: 99 },
  actions: { display: 'flex', gap: '0.25rem' },
  actionBtn: { padding: '0.25rem 0.5rem', background: '#28a745', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer', fontSize: '0.75rem' },
  deleteBtn: { padding: '0.25rem 0.5rem', background: '#dc3545', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer', fontSize: '0.75rem' },
};
