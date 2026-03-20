export function Button({ label, onClick }: { label: string; onClick?: () => void }) {
  return { type: "button", label, onClick };
}

export function Card({ title, children }: { title: string; children?: unknown }) {
  return { type: "card", title, children };
}
