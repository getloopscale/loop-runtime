document.addEventListener("DOMContentLoaded", function () {
  // Smooth scroll for anchor links
  var links = document.querySelectorAll('a[href^="#"]');
  links.forEach(function (link) {
    link.addEventListener("click", function (e) {
      e.preventDefault();
      var targetId = this.getAttribute("href").slice(1);
      var target = document.getElementById(targetId);
      if (target) {
        target.scrollIntoView({ behavior: "smooth" });
      }
    });
  });

  // Add active class to nav links on scroll
  var sections = document.querySelectorAll(".section");
  window.addEventListener("scroll", function () {
    var scrollPos = window.scrollY + 100;
    sections.forEach(function (section) {
      if (
        scrollPos >= section.offsetTop &&
        scrollPos < section.offsetTop + section.offsetHeight
      ) {
        var id = section.getAttribute("id");
        document.querySelectorAll(".nav-links a").forEach(function (a) {
          a.classList.remove("active");
          if (a.getAttribute("href") === "#" + id) {
            a.classList.add("active");
          }
        });
      }
    });
  });
});
