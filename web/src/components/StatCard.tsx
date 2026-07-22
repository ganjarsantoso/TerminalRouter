import React from 'react';

// Stats Card Component
export function StatCard({ title, value, icon }: { title: string, value: string | number, icon: React.ReactNode }) {
  return (
    <div className="glass-panel rounded-2xl border border-zinc-800/80 p-6 flex items-start justify-between">
      <div>
        <div className="text-xs font-semibold text-zinc-500 uppercase tracking-wider">{title}</div>
        <div className="text-2xl font-bold mt-2 text-zinc-100">{value}</div>
      </div>
      <div className="p-2 rounded-xl bg-zinc-900 border border-zinc-800">{icon}</div>
    </div>
  );
}
