const status = document.getElementById("status");

fetch("api/health")
  .then((response) => {
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`);
    }
    return response.json();
  })
  .then((health) => {
    status.textContent = `Appliance en service, version ${health.version}.`;
  })
  .catch((error) => {
    status.textContent = `Appliance injoignable : ${error.message}`;
  });
