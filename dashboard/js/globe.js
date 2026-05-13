const COLORS = {
  HIT:   0x34d399,
  STALE: 0xfbbf24,
  MISS:  0xef4444,
};

const EDGE_NODES = {
  virginia: { lat: 39.0481, lng: -77.4728 },
  ireland:  { lat: 53.3331, lng: -6.2489 },
  tokyo:    { lat: 35.6762, lng: 139.6503 },
};

class Globe {
  constructor(container) {
    this.container = container;
    this.arcs = [];
    this.points = [];
    this.clock = new THREE.Clock();
    this.init();
    this.animate();
  }

  init() {
    const w = this.container.clientWidth;
    const h = this.container.clientHeight;

    this.scene = new THREE.Scene();
    this.camera = new THREE.PerspectiveCamera(45, w / h, 0.1, 1000);
    this.camera.position.z = 2.8;

    this.renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    this.renderer.setSize(w, h);
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    this.container.appendChild(this.renderer.domElement);

    this.group = new THREE.Group();
    this.scene.add(this.group);

    this.createGlobe();
    this.createAtmosphere();
    this.createEdgeNodes();
    this.setupMouse();

    window.addEventListener('resize', () => this.onResize());
  }

  createGlobe() {
    const geo = new THREE.SphereGeometry(1, 64, 64);
    const mat = new THREE.MeshBasicMaterial({
      color: 0x0f1118,
      transparent: true,
      opacity: 0.95,
    });
    this.sphere = new THREE.Mesh(geo, mat);
    this.group.add(this.sphere);

    const wireGeo = new THREE.SphereGeometry(1.002, 36, 18);
    const wireMat = new THREE.MeshBasicMaterial({
      color: 0x1e2230,
      wireframe: true,
      transparent: true,
      opacity: 0.3,
    });
    this.group.add(new THREE.Mesh(wireGeo, wireMat));
  }

  createAtmosphere() {
    const geo = new THREE.SphereGeometry(1.06, 64, 64);
    const mat = new THREE.MeshBasicMaterial({
      color: 0x38bdf8,
      transparent: true,
      opacity: 0.05,
      side: THREE.BackSide,
    });
    this.group.add(new THREE.Mesh(geo, mat));
  }

  createEdgeNodes() {
    Object.entries(EDGE_NODES).forEach(([name, { lat, lng }]) => {
      const pos = this.latLngToVec3(lat, lng, 1.01);
      const geo = new THREE.SphereGeometry(0.015, 12, 12);
      const mat = new THREE.MeshBasicMaterial({ color: 0x38bdf8 });
      const mesh = new THREE.Mesh(geo, mat);
      mesh.position.copy(pos);
      this.group.add(mesh);
    });
  }

  addEvent(event) {
    if (!event.client_lat && !event.client_lng) return;

    const edgeNode = EDGE_NODES[event.edge_node];
    if (!edgeNode) return;

    const color = COLORS[event.cache_status] || COLORS.MISS;

    this.addPoint(event.client_lat, event.client_lng, color);
    this.addArc(event.client_lat, event.client_lng, edgeNode.lat, edgeNode.lng, color);
  }

  addPoint(lat, lng, color) {
    const pos = this.latLngToVec3(lat, lng, 1.01);
    const geo = new THREE.SphereGeometry(0.008, 8, 8);
    const mat = new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 1.0 });
    const mesh = new THREE.Mesh(geo, mat);
    mesh.position.copy(pos);
    this.group.add(mesh);

    this.points.push({ mesh, created: this.clock.getElapsedTime(), life: 4 });
  }

  addArc(fromLat, fromLng, toLat, toLng, color) {
    const start = this.latLngToVec3(fromLat, fromLng, 1.01);
    const end = this.latLngToVec3(toLat, toLng, 1.01);
    const mid = new THREE.Vector3().addVectors(start, end).multiplyScalar(0.5);
    const dist = start.distanceTo(end);
    mid.normalize().multiplyScalar(1 + dist * 0.4);

    const curve = new THREE.QuadraticBezierCurve3(start, mid, end);
    const points = curve.getPoints(50);
    const geo = new THREE.BufferGeometry().setFromPoints(points);
    const mat = new THREE.LineBasicMaterial({ color, transparent: true, opacity: 0.8 });
    const line = new THREE.Line(geo, mat);
    this.group.add(line);

    this.arcs.push({ line, created: this.clock.getElapsedTime(), life: 3 });
  }

  setupMouse() {
    let isDragging = false;
    let prevX = 0, prevY = 0;
    this.autoRotate = true;

    this.container.addEventListener('mousedown', (e) => {
      isDragging = true;
      prevX = e.clientX;
      prevY = e.clientY;
      this.autoRotate = false;
    });

    window.addEventListener('mousemove', (e) => {
      if (!isDragging) return;
      const dx = e.clientX - prevX;
      const dy = e.clientY - prevY;
      this.group.rotation.y += dx * 0.005;
      this.group.rotation.x += dy * 0.005;
      this.group.rotation.x = Math.max(-1, Math.min(1, this.group.rotation.x));
      prevX = e.clientX;
      prevY = e.clientY;
    });

    window.addEventListener('mouseup', () => {
      isDragging = false;
      setTimeout(() => { this.autoRotate = true; }, 3000);
    });
  }

  animate() {
    requestAnimationFrame(() => this.animate());

    if (this.autoRotate) {
      this.group.rotation.y += 0.001;
    }

    const now = this.clock.getElapsedTime();

    for (let i = this.arcs.length - 1; i >= 0; i--) {
      const arc = this.arcs[i];
      const age = now - arc.created;
      if (age > arc.life) {
        this.group.remove(arc.line);
        arc.line.geometry.dispose();
        arc.line.material.dispose();
        this.arcs.splice(i, 1);
      } else {
        arc.line.material.opacity = 0.8 * (1 - age / arc.life);
      }
    }

    for (let i = this.points.length - 1; i >= 0; i--) {
      const pt = this.points[i];
      const age = now - pt.created;
      if (age > pt.life) {
        this.group.remove(pt.mesh);
        pt.mesh.geometry.dispose();
        pt.mesh.material.dispose();
        this.points.splice(i, 1);
      } else {
        pt.mesh.material.opacity = 1 - age / pt.life;
        const scale = 1 + age * 0.3;
        pt.mesh.scale.set(scale, scale, scale);
      }
    }

    this.renderer.render(this.scene, this.camera);
  }

  onResize() {
    const w = this.container.clientWidth;
    const h = this.container.clientHeight;
    this.camera.aspect = w / h;
    this.camera.updateProjectionMatrix();
    this.renderer.setSize(w, h);
  }

  latLngToVec3(lat, lng, radius) {
    const phi = (90 - lat) * (Math.PI / 180);
    const theta = (lng + 180) * (Math.PI / 180);
    return new THREE.Vector3(
      -radius * Math.sin(phi) * Math.cos(theta),
      radius * Math.cos(phi),
      radius * Math.sin(phi) * Math.sin(theta)
    );
  }
}
