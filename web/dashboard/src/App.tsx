import { useWebSocket } from './hooks/useWebSocket';
import { useStateStore } from './stores/stateStore';

function App() {
  const { connected } = useWebSocket();
  const stats = useStateStore((state) => state.stats);
  const agents = useStateStore((state) => state.agents);

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100">
      <header className="bg-gray-800 border-b border-gray-700 px-6 py-4">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold">Canopy Dashboard</h1>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <div
                className={`w-2 h-2 rounded-full ${
                  connected ? 'bg-green-500' : 'bg-red-500'
                }`}
              />
              <span className="text-sm">
                {connected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <div className="bg-gray-800 rounded-lg p-6">
            <div className="text-sm text-gray-400 mb-1">Total Tasks</div>
            <div className="text-3xl font-bold">{stats.total_tasks}</div>
          </div>
          <div className="bg-gray-800 rounded-lg p-6">
            <div className="text-sm text-gray-400 mb-1">Running</div>
            <div className="text-3xl font-bold text-blue-500">
              {stats.running_tasks}
            </div>
          </div>
          <div className="bg-gray-800 rounded-lg p-6">
            <div className="text-sm text-gray-400 mb-1">Completed</div>
            <div className="text-3xl font-bold text-green-500">
              {stats.completed_tasks}
            </div>
          </div>
          <div className="bg-gray-800 rounded-lg p-6">
            <div className="text-sm text-gray-400 mb-1">Failed</div>
            <div className="text-3xl font-bold text-red-500">
              {stats.failed_tasks}
            </div>
          </div>
        </div>

        <div className="bg-gray-800 rounded-lg p-6">
          <h2 className="text-xl font-bold mb-4">Active Agents</h2>
          {Object.keys(agents).length === 0 ? (
            <p className="text-gray-400">No active agents</p>
          ) : (
            <div className="space-y-4">
              {Object.values(agents).map((agent) => (
                <div
                  key={agent.id}
                  className="bg-gray-700 rounded p-4 border border-gray-600"
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="font-medium">{agent.task_title}</div>
                    <div
                      className={`text-xs px-2 py-1 rounded ${
                        agent.status === 'running'
                          ? 'bg-blue-600'
                          : agent.status === 'completed'
                          ? 'bg-green-600'
                          : agent.status === 'failed'
                          ? 'bg-red-600'
                          : 'bg-gray-600'
                      }`}
                    >
                      {agent.status}
                    </div>
                  </div>
                  <div className="text-sm text-gray-400">
                    Agent ID: {agent.id}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

export default App;
