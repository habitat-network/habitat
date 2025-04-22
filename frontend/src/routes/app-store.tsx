import { getAvailableAppsWithInstallStatus, installApp } from "@/api/node";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute('/app-store')({
  async loader() {
    return getAvailableAppsWithInstallStatus()
  },
  component() {
    const data = Route.useLoaderData()
    const { mutate: handleInstall } = useMutation({
      mutationFn(app: any) {
        return installApp(app)
      }
    })
    return <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Version</th>
          <th>Driver</th>
        </tr>
      </thead>
      <tbody>
        {data.map((app: any) => (
          <tr key={app.id}>
            <td>{app.app_installation.name}</td>
            <td>{app.app_installation.version}</td>
            <td>{app.app_installation.driver}</td>
            <td>
              {app.installed ? <button disabled>Installed</button> :
                <button onClick={() => handleInstall(app)}>Install</button>}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  }
});
